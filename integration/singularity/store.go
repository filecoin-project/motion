package singularity

import (
	"context"
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
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/motion/blob"
	"github.com/ipfs/go-log/v2"
)

var logger = log.Logger("motion/integration/singularity")

type SingularityStore struct {
	*options
	local    *blob.LocalStore
	sourceID uint32
	toPack   chan uint64
	closing  chan struct{}
	closed   chan struct{}
}

func NewStore(o ...Option) (*SingularityStore, error) {
	opts, err := newOptions(o...)
	if err != nil {
		logger.Errorw("Failed to instantiate options", "err", err)
		return nil, err
	}
	return &SingularityStore{
		options: opts,
		local:   blob.NewLocalStore(opts.storeDir),
		toPack:  make(chan uint64, 16),
		closing: make(chan struct{}),
		closed:  make(chan struct{}),
	}, nil
}

func (l *SingularityStore) Start(ctx context.Context) error {
	logger := logger.With("dataset", l.datasetName)
	ds, err := l.singularityClient.CreateDataset(ctx, dataset.CreateRequest{
		Name:       l.datasetName,
		MaxSizeStr: l.maxCarSize,
	})
	switch {
	case err == nil:
		logger.Infow("Successfully created motion dataset on singularity", "id", ds.ID)
	case errors.As(err, &client.DuplicateRecordError{}):
		logger.Info("Skipping motion dataset creation; already exists.")
	default:
		logger.Errorw("Failed to create motion dataset on singularity", "err", err)
		return err
	}

	// find or create the motion datasource
	sources, err := l.singularityClient.ListSourcesByDataset(ctx, l.datasetName)
	if err != nil {
		logger.Errorw("Failed to list sources for dataset", "err", err)
		return fmt.Errorf("failed to list sources by dataset: %w", err)
	}
	found := false
	for _, source := range sources {
		if source.Type == "local" && source.Path == strings.TrimSuffix(l.local.Dir(), "/") {
			l.sourceID = source.ID
			found = true
			logger.Infow("Found source on singularity.", "id", source.ID)
			break
		}
	}
	if !found {
		logger.Info("Source not found on singularity. Created...")
		source, err := l.singularityClient.CreateLocalSource(ctx, l.datasetName, datasource.LocalRequest{
			SourcePath:        l.local.Dir(),
			RescanInterval:    "0",
			DeleteAfterExport: false,
			ScanningState:     model.Created,
		})
		if err != nil {
			logger.Errorw("Failed to create local source on singularity", "err", err)
			return fmt.Errorf("failed to create local source: %w", err)
		}
		l.sourceID = source.ID
		logger.Infow("Successfully created  local source on singularity", "id", source.ID)
	}

	// insure default wallet is imported to singularity
	wallets, err := l.singularityClient.ListWallets(ctx)
	if err != nil {
		logger.Errorw("Failed to list singularity wallets", "err", err)
		return fmt.Errorf("failed to list singularity wallets: %w", err)
	}
	var wlt *model.Wallet
	for _, existing := range wallets {
		if existing.PrivateKey == l.walletKey {
			wlt = &existing
			logger.Infow("Wallet found on singularity", "id", existing.ID)
			break
		}
	}
	if wlt == nil {
		logger.Info("Wallet is not found on singularity. Importing...")
		wlt, err = l.singularityClient.ImportWallet(ctx, wallethandler.ImportRequest{
			PrivateKey: l.walletKey,
		})
		if err != nil {
			logger.Errorw("Failed to import wallet to singularity", "err", err)
			return fmt.Errorf("failed to import wallet: %w", err)
		}
	}

	// insure wallet is assigned to dataset
	wallets, err = l.singularityClient.ListWalletsByDataset(ctx, l.datasetName)
	if err != nil {
		return nil
	}
	walletFound := false
	for _, existing := range wallets {
		if existing.Address == wlt.Address {
			logger.Infow("Wallet for dataset found on singularity", "id", existing.ID)
			walletFound = true
			break
		}
	}
	if !walletFound {
		logger.Info("Wallet was not found. Creating...")
		if wlt, err := l.singularityClient.AddWalletToDataset(ctx, l.datasetName, wlt.Address); err != nil {
			logger.Errorw("Failed to add wallet to dataset", "err", err)
			return err
		} else {
			logger.Infow("Successfully added wallet to dataset", "id", wlt.ID)
		}
	}
	// insure schedules are created
	// TODO: handle config changes for replication -- singularity currently has no modify schedule endpoint
	schedules, err := l.singularityClient.ListSchedulesByDataset(ctx, l.datasetName)
	switch {
	case err == nil:
		logger.Infow("Found existing schedules for dataset", "count", len(schedules))
	case errors.As(err, &client.NotFoundError{}):
		logger.Info("Found no schedules for dataset")
	default:
		logger.Errorw("Failed to list schedules for dataset", "err", err)
		return err
	}
	pricePerGBEpoch, _ := (new(big.Rat).SetFrac(l.pricePerGiBEpoch.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerGB, _ := (new(big.Rat).SetFrac(l.pricePerGiB.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerDeal, _ := (new(big.Rat).SetFrac(l.pricePerDeal.Int, big.NewInt(int64(1e18)))).Float64()

	for _, sp := range l.storageProviders {
		var foundSchedule bool
		logger := logger.With("provider", sp)
		for _, schd := range schedules {
			scheduleAddr, err := address.NewFromString(schd.Provider)
			if err == nil && sp == scheduleAddr {
				foundSchedule = true
				logger.Infow("Schedule found for provider", "id", schd.ID)
				break
			}
		}
		if !foundSchedule {
			logger.Info("Schedule not found for provider. Creating...")
			if schd, err := l.singularityClient.CreateSchedule(ctx, schedule.CreateRequest{
				DatasetName:           l.datasetName,
				Provider:              sp.String(),
				PricePerGBEpoch:       pricePerGBEpoch,
				PricePerGB:            pricePerGB,
				PricePerDeal:          pricePerDeal,
				Verified:              l.verifiedDeal,
				IPNI:                  l.ipniAnnounce,
				KeepUnsealed:          l.keepUnsealed,
				StartDelay:            strconv.Itoa(int(l.dealStartDelay)*builtin.EpochDurationSeconds) + "s",
				Duration:              strconv.Itoa(int(l.dealDuration)*builtin.EpochDurationSeconds) + "s",
				ScheduleCron:          l.scheduleCron,
				ScheduleCronPerpetual: l.scheduleCronPerpetual,
				ScheduleDealNumber:    l.scheduleDealNumber,
				TotalDealNumber:       l.totalDealNumber,
				ScheduleDealSize:      l.scheduleDealSize,
				TotalDealSize:         l.totalDealSize,
				MaxPendingDealSize:    l.maxPendingDealSize,
				MaxPendingDealNumber:  l.maxPendingDealNumber,
				URLTemplate:           l.scheduleUrlTemplate,
			}); err != nil {
				logger.Errorw("Failed to create schedule for provider", "err", err)
				return fmt.Errorf("failed to create schedule: %w", err)
			} else {
				logger.Infow("Successfully created schedule for provider", "id", schd.ID)
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
				if outstanding > l.packThreshold {
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

func (s *SingularityStore) Put(ctx context.Context, reader io.ReadCloser) (*blob.Descriptor, error) {
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
	idFile, err := os.CreateTemp(s.local.Dir(), "motion_local_store_*.bin.temp")
	if err != nil {
		return nil, err
	}
	defer idFile.Close()
	_, err = idFile.Write([]byte(strconv.FormatUint(file.ID, 10)))
	if err != nil {
		_ = os.Remove(idFile.Name())
		return nil, err
	}
	if err = os.Rename(idFile.Name(), path.Join(s.local.Dir(), desc.ID.String()+".id")); err != nil {
		return nil, err
	}
	return desc, nil
}

func (s *SingularityStore) Get(ctx context.Context, id blob.ID) (io.ReadSeekCloser, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.Dir(), id.String()+".id"))
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
		return nil, blob.ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded blob.ID
	err = decoded.Decode(strings.TrimSuffix(path.Base(item.Path), path.Ext(item.Path)))
	if err != nil {
		return nil, err
	}
	return s.local.Get(ctx, decoded)
}

func (s *SingularityStore) Describe(ctx context.Context, id blob.ID) (*blob.Descriptor, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.Dir(), id.String()+".id"))
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
		return nil, blob.ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded blob.ID
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
	replicas := make([]blob.Replica, 0, len(deals))
	for _, deal := range deals {
		replicas = append(replicas, blob.Replica{
			// TODO: figure out how to get LastVerified
			Provider:   deal.Provider,
			Status:     string(deal.State),
			Expiration: epochutil.EpochToTime(deal.EndEpoch),
		})
	}
	descriptor.Status = &blob.Status{
		Replicas: replicas,
	}
	return descriptor, nil
}
