package blob

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/data-preservation-programs/singularity/client"
	"github.com/data-preservation-programs/singularity/handler/dataset"
	"github.com/data-preservation-programs/singularity/handler/datasource"
	"github.com/data-preservation-programs/singularity/handler/deal/schedule"
	wallethandler "github.com/data-preservation-programs/singularity/handler/wallet"
	"github.com/data-preservation-programs/singularity/model"
	"github.com/data-preservation-programs/singularity/service/epochutil"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/motion/replicationconfig"
	"github.com/filecoin-project/motion/wallet"
)

const motionDatasetName = "MOTION_DATASET"

func maxCarSize() string {
	if address.CurrentNetwork == address.Testnet {
		return "7.5Mib"
	}
	return "31.5GiB"
}

func packThreshold() int64 {
	if address.CurrentNetwork == address.Testnet {
		return 1 << 12
	}
	return 1 << 34
}

type SingularityStore struct {
	local             *LocalStore
	sourceID          uint32
	wallet            *wallet.Wallet
	singularityClient client.Client
	toPack            chan uint64
	closing           chan struct{}
	closed            chan struct{}
	replicationConfig *replicationconfig.ReplicationConfig
}

func NewSingularityStore(dir string, wallet *wallet.Wallet, replicationConfig *replicationconfig.ReplicationConfig, singularityClient client.Client) *SingularityStore {
	local := NewLocalStore(dir)
	return &SingularityStore{
		local:             local,
		wallet:            wallet,
		singularityClient: singularityClient,
		replicationConfig: replicationConfig,
		toPack:            make(chan uint64, 16),
		closing:           make(chan struct{}),
		closed:            make(chan struct{}),
	}
}

func (l *SingularityStore) Start(ctx context.Context) error {
	_, err := l.singularityClient.CreateDataset(ctx, dataset.CreateRequest{
		Name:       motionDatasetName,
		MaxSizeStr: maxCarSize(),
	})
	var asDuplicatedRecord client.DuplicateRecordError

	// return errors, but ignore duplicated record, that means we just already created it
	if err != nil && !errors.As(err, &asDuplicatedRecord) {
		return err
	}

	// find or create the motion datasource
	sources, err := l.singularityClient.ListSourcesByDataset(ctx, motionDatasetName)
	if err != nil {
		return err
	}
	found := false
	for _, source := range sources {
		if source.Type == "local" && source.Path == strings.TrimSuffix(l.local.dir, "/") {
			l.sourceID = source.ID
			found = true
			break
		}
	}
	if !found {
		source, err := l.singularityClient.CreateLocalSource(ctx, motionDatasetName, datasource.LocalRequest{
			SourcePath:        l.local.dir,
			RescanInterval:    "0",
			DeleteAfterExport: false,
			ScanningState:     model.Created,
		})
		if err != nil {
			return err
		}
		l.sourceID = source.ID
	}
	// get default wallet encoded private key
	walletAddr, err := l.wallet.GetDefault()
	if err != nil {
		return err
	}
	ki, err := l.wallet.WalletExport(ctx, walletAddr)
	if err != nil {
		return err
	}

	b, err := json.Marshal(ki)
	if err != nil {
		return err
	}

	pk := hex.EncodeToString(b)

	// insure default wallet is imported to singularity
	wallets, err := l.singularityClient.ListWallets(ctx)
	if err != nil {
		return err
	}
	var wallet *model.Wallet
	for _, existing := range wallets {
		if wallet.PrivateKey == pk {
			wallet = &existing
			break
		}
	}
	if wallet == nil {
		wallet, err = l.singularityClient.ImportWallet(ctx, wallethandler.ImportRequest{
			PrivateKey: pk,
		})

		if err != nil {
			return err
		}
	}

	// insure wallet is assigned to dataset
	wallets, err = l.singularityClient.ListWalletsByDataset(ctx, motionDatasetName)
	if err != nil {
		return nil
	}
	walletFound := false
	for _, existing := range wallets {
		if existing.Address == wallet.Address {
			walletFound = true
			break
		}
	}
	if !walletFound {
		_, err := l.singularityClient.AddWalletToDataset(ctx, motionDatasetName, wallet.Address)
		if err != nil {
			return err
		}
	}
	// insure schedules are created
	// TODO: handle config changes for replication -- singularity currently has no modify schedule endpoint
	schedules, err := l.singularityClient.ListSchedulesByDataset(ctx, motionDatasetName)
	if err != nil {
		return err
	}
	pricePerGBEpoch, _ := (new(big.Rat).SetFrac(l.replicationConfig.PricePerGiBEpoch.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerGB, _ := (new(big.Rat).SetFrac(l.replicationConfig.PricePerGiB.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerDeal, _ := (new(big.Rat).SetFrac(l.replicationConfig.PricePerDeal.Int, big.NewInt(int64(1e18)))).Float64()

	for _, sp := range l.replicationConfig.StorageProviders {
		var foundSchedule bool
		for _, schedule := range schedules {
			scheduleAddr, err := address.NewFromString(schedule.Provider)
			if err == nil && sp == scheduleAddr {
				foundSchedule = true
				break
			}
		}
		if !foundSchedule {
			_, err := l.singularityClient.CreateSchedule(ctx, schedule.CreateRequest{
				DatasetName:     motionDatasetName,
				Provider:        sp.String(),
				PricePerGBEpoch: pricePerGBEpoch,
				PricePerGB:      pricePerGB,
				PricePerDeal:    pricePerDeal,
				StartDelay:      strconv.Itoa(int(l.replicationConfig.DealStartDelay)*30) + "s",
				Duration:        strconv.Itoa(int(l.replicationConfig.DealDuration)*30) + "s",
			})
			if err != nil {
				return err
			}
		}
	}
	go l.runPreparationJobs()
	return nil
}

func (l *SingularityStore) runPreparationJobs() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer func() {
			close(l.closed)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case fileID := <-l.toPack:
				outstanding, err := l.singularityClient.PrepareToPackFile(ctx, fileID)
				if err != nil {
					logger.Errorw("preparing to pack file", "fileID", fileID, "error", err)
				}
				if outstanding > packThreshold() {
					// mark outstanding pack jobs as ready to go so we can make CAR files
					err := l.singularityClient.PrepareToPackSource(ctx, l.sourceID)
					if err != nil {
						logger.Errorw("preparing to pack source", "error", err)
					}
				}
			}
		}
	}()
	<-l.closing
}

func (l *SingularityStore) Shutdown(ctx context.Context) error {
	close(l.closing)
	select {
	case <-ctx.Done():
	case <-l.closed:
	}
	return nil
}

func (s *SingularityStore) Put(ctx context.Context, reader io.ReadCloser) (*Descriptor, error) {
	desc, err := s.local.Put(ctx, reader)
	if err != nil {
		return nil, err
	}
	file, err := s.singularityClient.PushFile(ctx, s.sourceID, datasource.FileInfo{Path: desc.ID.String() + ".bin"})
	if err != nil {
		return nil, fmt.Errorf("error creating singularity entry: %w", err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.toPack <- file.ID:
	}
	idFile, err := os.CreateTemp(s.local.dir, "motion_local_store_*.bin.temp")
	if err != nil {
		return nil, err
	}
	defer idFile.Close()
	_, err = idFile.Write([]byte(strconv.FormatUint(file.ID, 10)))
	if err != nil {
		_ = os.Remove(idFile.Name())
		return nil, err
	}
	if err = os.Rename(idFile.Name(), path.Join(s.local.dir, desc.ID.String()+".id")); err != nil {
		return nil, err
	}
	return desc, nil
}

func (s *SingularityStore) Get(ctx context.Context, id ID) (io.ReadSeekCloser, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.dir, id.String()+".id"))
	if err != nil {
		return nil, err
	}
	itemIDString, err := io.ReadAll(idStream)
	if err != nil {
		return nil, err
	}
	itemID, err := strconv.ParseUint(string(itemIDString), 10, 64)
	if err != nil {
		return nil, err
	}
	item, err := s.singularityClient.GetFile(ctx, itemID)
	var asNotFoundError client.NotFoundError
	if errors.As(err, &asNotFoundError) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded ID
	err = decoded.Decode(strings.TrimSuffix(path.Base(item.Path), path.Ext(item.Path)))
	if err != nil {
		return nil, err
	}
	return s.local.Get(ctx, decoded)
}

func (s *SingularityStore) Describe(ctx context.Context, id ID) (*Descriptor, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.dir, id.String()+".id"))
	if err != nil {
		return nil, err
	}
	itemIDString, err := io.ReadAll(idStream)
	if err != nil {
		return nil, err
	}
	itemID, err := strconv.ParseUint(string(itemIDString), 10, 64)
	if err != nil {
		return nil, err
	}
	item, err := s.singularityClient.GetFile(ctx, itemID)
	var asNotFoundError client.NotFoundError
	if errors.As(err, &asNotFoundError) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded ID
	err = decoded.Decode(strings.TrimSuffix(path.Base(item.Path), path.Ext(item.Path)))
	if err != nil {
		return nil, err
	}
	descriptor, err := s.local.Describe(ctx, decoded)
	if err != nil {
		return nil, err
	}
	deals, err := s.singularityClient.GetFileDeals(ctx, itemID)
	if err != nil {
		return nil, err
	}
	replicas := make([]Replica, 0, len(deals))
	for _, deal := range deals {
		replicas = append(replicas, Replica{
			// TODO: figure out how to get LastVerified
			Provider:   deal.Provider,
			Status:     string(deal.State),
			Expiration: epochutil.EpochToTime(deal.EndEpoch),
		})
	}
	descriptor.Status = &Status{
		Replicas: replicas,
	}
	return descriptor, nil
}
