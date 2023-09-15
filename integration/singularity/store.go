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
	"sync/atomic"
	"time"

	"github.com/data-preservation-programs/singularity/client/swagger/http/deal_schedule"
	"github.com/data-preservation-programs/singularity/client/swagger/http/file"
	"github.com/data-preservation-programs/singularity/client/swagger/http/job"
	"github.com/data-preservation-programs/singularity/client/swagger/http/preparation"
	"github.com/data-preservation-programs/singularity/client/swagger/http/storage"
	"github.com/data-preservation-programs/singularity/client/swagger/http/wallet"
	"github.com/data-preservation-programs/singularity/client/swagger/http/wallet_association"
	"github.com/data-preservation-programs/singularity/client/swagger/models"
	"github.com/data-preservation-programs/singularity/service/epochutil"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/motion/blob"
	"github.com/gotidy/ptr"
	"github.com/ipfs/go-log/v2"
)

var logger = log.Logger("motion/integration/singularity")

type SingularityStore struct {
	*options
	local         *blob.LocalStore
	sourceName    string
	toPack        chan uint64
	closing       chan struct{}
	closed        chan struct{}
	cleanupActive atomic.Bool
}

func NewStore(o ...Option) (*SingularityStore, error) {
	opts, err := newOptions(o...)
	if err != nil {
		logger.Errorw("Failed to instantiate options", "err", err)
		return nil, err
	}
	s := &SingularityStore{
		options:    opts,
		local:      blob.NewLocalStore(opts.storeDir),
		sourceName: "source",
		toPack:     make(chan uint64, 16),
		closing:    make(chan struct{}),
		closed:     make(chan struct{}),
	}
	go s.runCleanupWorker(context.Background())
	return s, nil
}

func (l *SingularityStore) initPreparation(ctx context.Context) (*models.ModelPreparation, error) {
	createSourceStorageRes, err := l.singularityClient.Storage.CreateLocalStorage(&storage.CreateLocalStorageParams{
		Context: ctx,
		Request: &models.StorageCreateLocalStorageRequest{
			Name: l.sourceName,
			Path: l.local.Dir(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create source storage: %w", err)
	}
	logger.Infow("Created source storage", "id", createSourceStorageRes.Payload.ID)

	createPreparationRes, err := l.singularityClient.Preparation.CreatePreparation(&preparation.CreatePreparationParams{
		Context: ctx,
		Request: &models.DataprepCreateRequest{
			MaxSize:        &l.maxCarSize,
			Name:           l.preparationName,
			SourceStorages: []string{l.sourceName},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create preparation: %w", err)
	}
	logger.Infow("Created preparation", "id", createPreparationRes.Payload.ID)

	return createPreparationRes.Payload, nil
}

func (l *SingularityStore) Start(ctx context.Context) error {
	logger := logger.With("preparation", l.preparationName)

	// List out preparations and see if one with the configured name exists

	listPreparationsRes, err := l.singularityClient.Preparation.ListPreparations(&preparation.ListPreparationsParams{
		Context: ctx,
	})
	if err != nil {
		logger.Errorw("Failed to list preparations", "err", err)
		return fmt.Errorf("failed to list preparations: %w", err)
	}

	var preparation *models.ModelPreparation
	for _, preparationCmp := range listPreparationsRes.Payload {
		if preparationCmp.Name == l.preparationName {
			preparation = preparationCmp
			break
		}
	}
	if preparation == nil {
		// If no preparation was found, initialize it
		_, err = l.initPreparation(ctx)
		if err != nil {
			logger.Errorw("First-time preparation initialization failed", "err", err)
			return fmt.Errorf("first-time preparation initialization failed: %w", err)
		}
	}

	// Ensure default wallet is imported to singularity
	listWalletsRes, err := l.singularityClient.Wallet.ListWallets(&wallet.ListWalletsParams{
		Context: ctx,
	})
	if err != nil {
		logger.Errorw("Failed to list singularity wallets", "err", err)
		return fmt.Errorf("failed to list singularity wallets: %w", err)
	}
	var wlt *models.ModelWallet
	for _, existing := range listWalletsRes.Payload {
		if existing.PrivateKey == l.walletKey {
			wlt = existing
			logger.Infow("Wallet found on singularity", "id", existing.ID)
			break
		}
	}
	if wlt == nil {
		logger.Info("Wallet is not found on singularity. Importing...")
		importWalletRes, err := l.singularityClient.Wallet.ImportWallet(&wallet.ImportWalletParams{
			Context: ctx,
			Request: &models.WalletImportRequest{
				PrivateKey: l.walletKey,
			},
		})
		if err != nil {
			logger.Errorw("Failed to import wallet to singularity", "err", err)
			return fmt.Errorf("failed to import wallet: %w", err)
		}

		wlt = importWalletRes.Payload
	}

	// Ensure wallet is assigned to preparation
	listAttachedWalletsRes, err := l.singularityClient.WalletAssociation.ListAttachedWallets(&wallet_association.ListAttachedWalletsParams{
		Context: ctx,
		ID:      l.preparationName,
	})
	if err != nil {
		return err
	}
	walletFound := false
	for _, existing := range listAttachedWalletsRes.Payload {
		if existing.Address == wlt.Address {
			logger.Infow("Wallet for preparation found on singularity", "id", existing.ID)
			walletFound = true
			break
		}
	}
	if !walletFound {
		logger.Info("Wallet was not found. Creating...")
		if attachWalletRes, err := l.singularityClient.WalletAssociation.AttachWallet(&wallet_association.AttachWalletParams{
			Context: ctx,
			ID:      l.preparationName,
			Wallet:  wlt.Address,
		}); err != nil {
			logger.Errorw("Failed to add wallet to preparation", "err", err)
			return err
		} else {
			logger.Infow("Successfully added wallet to preparation", "id", attachWalletRes.Payload.ID)
		}
	}
	// Ensure schedules are created
	// TODO: handle config changes for replication -- singularity currently has no modify schedule endpoint
	listPreparationSchedulesRes, err := l.singularityClient.DealSchedule.ListPreparationSchedules(&deal_schedule.ListPreparationSchedulesParams{
		Context: ctx,
		ID:      l.preparationName,
	})

	switch {
	case err == nil:
		logger.Infow("Found existing schedules for preparation", "count", len(listPreparationSchedulesRes.Payload))
	case strings.Contains(err.Error(), "404"):
		logger.Info("Found no schedules for preparation")
	default:
		logger.Errorw("Failed to list schedules for preparation", "err", err)
		return err
	}
	pricePerGBEpoch, _ := (new(big.Rat).SetFrac(l.pricePerGiBEpoch.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerGB, _ := (new(big.Rat).SetFrac(l.pricePerGiB.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerDeal, _ := (new(big.Rat).SetFrac(l.pricePerDeal.Int, big.NewInt(int64(1e18)))).Float64()

	logger.Infof("Checking %v storage providers", len(l.storageProviders))
	for _, sp := range l.storageProviders {
		logger.Infof("Checking storage provider %s", sp)
		var foundSchedule bool
		logger := logger.With("provider", sp)
		for _, schd := range listPreparationSchedulesRes.Payload {
			scheduleAddr, err := address.NewFromString(schd.Provider)
			if err == nil && sp == scheduleAddr {
				foundSchedule = schd
				break
			}
		}
		if foundSchedule != nil {
			// If schedule was found, update it
			logger.Infow("Schedule found for provider. Updating with latest settings...", "id", foundSchedule.ID)
			_, err := l.singularityClient.DealSchedule.UpdateSchedule(&deal_schedule.UpdateScheduleParams{
				Context: ctx,
				ID:      foundSchedule.ID,
				Body: &models.ScheduleUpdateRequest{
					PricePerGbEpoch:       pricePerGBEpoch,
					PricePerGb:            pricePerGB,
					PricePerDeal:          pricePerDeal,
					Verified:              &l.verifiedDeal,
					Ipni:                  &l.ipniAnnounce,
					KeepUnsealed:          &l.keepUnsealed,
					StartDelay:            ptr.String(strconv.Itoa(int(l.dealStartDelay)*builtin.EpochDurationSeconds) + "s"),
					Duration:              ptr.String(strconv.Itoa(int(l.dealDuration)*builtin.EpochDurationSeconds) + "s"),
					ScheduleCron:          l.scheduleCron,
					ScheduleCronPerpetual: l.scheduleCronPerpetual,
					ScheduleDealNumber:    int64(l.scheduleDealNumber),
					TotalDealNumber:       int64(l.totalDealNumber),
					ScheduleDealSize:      l.scheduleDealSize,
					TotalDealSize:         l.totalDealSize,
					MaxPendingDealSize:    l.maxPendingDealSize,
					MaxPendingDealNumber:  int64(l.maxPendingDealNumber),
					URLTemplate:           l.scheduleUrlTemplate,
				},
			})
			if err != nil {
				logger.Errorw("Failed to update schedule for provider", "err", err)
				return fmt.Errorf("failed to update schedule: %w", err)
			}
		} else {
			// Otherwise, create it
			logger.Info("Schedule not found for provider. Creating...")
			if createScheduleRes, err := l.singularityClient.DealSchedule.CreateSchedule(&deal_schedule.CreateScheduleParams{
				Context: ctx,
				Schedule: &models.ScheduleCreateRequest{
					Preparation:           l.preparationName,
					Provider:              sp.String(),
					PricePerGbEpoch:       pricePerGBEpoch,
					PricePerGb:            pricePerGB,
					PricePerDeal:          pricePerDeal,
					Verified:              &l.verifiedDeal,
					Ipni:                  &l.ipniAnnounce,
					KeepUnsealed:          &l.keepUnsealed,
					StartDelay:            ptr.String(strconv.Itoa(int(l.dealStartDelay)*builtin.EpochDurationSeconds) + "s"),
					Duration:              ptr.String(strconv.Itoa(int(l.dealDuration)*builtin.EpochDurationSeconds) + "s"),
					ScheduleCron:          l.scheduleCron,
					ScheduleCronPerpetual: l.scheduleCronPerpetual,
					ScheduleDealNumber:    int64(l.scheduleDealNumber),
					TotalDealNumber:       int64(l.totalDealNumber),
					ScheduleDealSize:      l.scheduleDealSize,
					TotalDealSize:         l.totalDealSize,
					MaxPendingDealSize:    l.maxPendingDealSize,
					MaxPendingDealNumber:  int64(l.maxPendingDealNumber),
					URLTemplate:           l.scheduleUrlTemplate,
				},
			}); err != nil {
				logger.Errorw("Failed to create schedule for provider", "err", err)
				return fmt.Errorf("failed to create schedule: %w", err)
			} else {
				logger.Infow("Successfully created schedule for provider", "id", createScheduleRes.Payload.ID)
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
				prepareToPackSourceRes, err := l.singularityClient.File.PrepareToPackFile(&file.PrepareToPackFileParams{
					Context: ctx,
					ID:      int64(fileID),
				})
				if err != nil {
					logger.Errorw("preparing to pack file", "fileID", fileID, "error", err)
				}
				if prepareToPackSourceRes.Payload > l.packThreshold {
					// mark outstanding pack jobs as ready to go so we can make CAR files
					_, err := l.singularityClient.Job.PrepareToPackSource(&job.PrepareToPackSourceParams{
						Context: ctx,
						ID:      l.preparationName,
						Name:    l.sourceName,
					})
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
		logger.Errorw("Failed to store file locally", "err", err)
		return nil, fmt.Errorf("failed to put file locally: %w", err)
	}
	filePath := desc.ID.String() + ".bin"
	pushFileRes, err := s.singularityClient.File.PushFile(&file.PushFileParams{
		Context: ctx,
		File:    &models.FileInfo{Path: filePath},
		ID:      s.preparationName,
		Name:    s.sourceName,
	})
	if err != nil {
		logger.Errorw("Failed to push file to singularity", "path", filePath, "err", err)
		return nil, fmt.Errorf("error creating singularity entry: %w", err)
	}
	select {
	case <-ctx.Done():
		err := ctx.Err()
		logger.Errorw("Context done while putting file", "err", err)
		return nil, err
	case s.toPack <- uint64(pushFileRes.Payload.ID):
	}
	idFile, err := os.CreateTemp(s.local.Dir(), "motion_local_store_*.bin.temp")
	if err != nil {
		logger.Errorw("Failed to create temporary file", "err", err)
		return nil, err
	}
	defer func() {
		if err := idFile.Close(); err != nil {
			logger.Debugw("Failed to close temporary file", "err", err)
		}
	}()
	_, err = idFile.Write([]byte(strconv.FormatUint(uint64(pushFileRes.Payload.ID), 10)))
	if err != nil {
		if err := os.Remove(idFile.Name()); err != nil {
			logger.Debugw("Failed to remove temporary file", "path", idFile.Name(), "err", err)
		}
		logger.Errorw("Failed to write ID file", "err", err)
		return nil, err
	}
	if err = os.Rename(idFile.Name(), path.Join(s.local.Dir(), desc.ID.String()+".id")); err != nil {
		logger.Errorw("Failed to move ID file to store", "err", err)
		return nil, err
	}
	logger.Infow("Stored blob successfully", "id", desc.ID, "size", desc.Size, "singularityFileID", pushFileRes.Payload.ID)
	return desc, nil
}

func (s *SingularityStore) Get(ctx context.Context, id blob.ID) (io.ReadSeekCloser, error) {
	idStream, err := os.Open(path.Join(s.local.Dir(), id.String()+".id"))
	if err != nil {
		return nil, err
	}
	fileIDString, err := io.ReadAll(idStream)
	if err != nil {
		return nil, err
	}
	fileID, err := strconv.ParseUint(string(fileIDString), 10, 64)
	if err != nil {
		return nil, err
	}
	getFileRes, err := s.singularityClient.File.GetFile(&file.GetFileParams{
		Context: ctx,
		ID:      int64(fileID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, blob.ErrBlobNotFound
		}

		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}

	return &SingularityReader{
		client: s.singularityClient,
		fileID: fileID,
		offset: 0,
		size:   getFileRes.Payload.Size,
	}, nil
}

func (s *SingularityStore) Describe(ctx context.Context, id blob.ID) (*blob.Descriptor, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.Dir(), id.String()+".id"))
	if err != nil {
		return nil, err
	}
	fileIDString, err := io.ReadAll(idStream)
	if err != nil {
		return nil, err
	}
	fileID, err := strconv.ParseUint(string(fileIDString), 10, 64)
	if err != nil {
		return nil, err
	}
	getFileRes, err := s.singularityClient.File.GetFile(&file.GetFileParams{
		Context: ctx,
		ID:      int64(fileID),
	})
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, blob.ErrBlobNotFound
		}

		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded blob.ID
	err = decoded.Decode(strings.TrimSuffix(path.Base(getFileRes.Payload.Path), path.Ext(getFileRes.Payload.Path)))
	if err != nil {
		return nil, err
	}
	descriptor, err := s.local.Describe(ctx, decoded)
	if err != nil {
		return nil, err
	}
	getFileDealsRes, err := s.singularityClient.File.GetFileDeals(&file.GetFileDealsParams{
		Context: ctx,
		ID:      int64(fileID),
	})
	if err != nil {
		return nil, err
	}
	replicas := make([]blob.Replica, 0, len(getFileDealsRes.Payload))
	for _, deal := range getFileDealsRes.Payload {
		replicas = append(replicas, blob.Replica{
			// TODO: figure out how to get LastVerified
			Provider:   deal.Provider,
			Status:     string(deal.State),
			Expiration: epochutil.EpochToTime(int32(deal.EndEpoch)),
		})
	}
	descriptor.Status = &blob.Status{
		Replicas: replicas,
	}
	return descriptor, nil
}

func (s *SingularityStore) runCleanupWorker(ctx context.Context) {
	// Run immediately once before starting ticker
	go func() {
		if err := s.cleanup(context.Background()); err != nil {
			logger.Errorf("Local store cleanup failed: %w", err)
		}
	}()

	ticker := time.NewTicker(s.localCleanupInterval)
cleanupLoop:
	for {
		select {
		case <-ticker.C:
			go func() {
				if err := s.cleanup(context.Background()); err != nil {
					logger.Errorf("Local store cleanup failed: %w", err)
				}
			}()
		case <-s.closing:
			break cleanupLoop
		}
	}
}

var errCleanupAlreadyRunning = errors.New("cleanup already running")

func (s *SingularityStore) cleanup(ctx context.Context) error {
	if s.cleanupActive.Load() {
		return errCleanupAlreadyRunning
	}

	// Mark cleanup active for the duration of the function
	s.cleanupActive.Store(true)
	defer s.cleanupActive.Store(false)

	logger.Infof("Starting local store cleanup...")

	dir, err := os.ReadDir(s.local.Dir())
	if err != nil {
		return fmt.Errorf("failed to open local store directory: %w", err)
	}

	var binsToDelete []string

binIteration:
	for _, entry := range dir {
		binFileName := entry.Name()

		id, entryIsBin := strings.CutSuffix(binFileName, ".bin")
		if !entryIsBin {
			continue
		}

		idFileName := id + ".id"
		idStream, err := os.Open(path.Join(s.local.Dir(), idFileName))
		if err != nil {
			logger.Warnf("Failed to open ID map file for %s: %v", id, err)
			continue
		}
		fileIDString, err := io.ReadAll(idStream)
		if err != nil {
			logger.Warnf("Failed to read ID map file for %s: %v", id, err)
			continue
		}
		fileID, err := strconv.ParseUint(string(fileIDString), 10, 64)
		if err != nil {
			logger.Warnf("Failed to parse file ID %s as integer: %v", fileIDString, err)
			continue
		}

		getFileDealsRes, err := s.singularityClient.File.GetFileDeals(&file.GetFileDealsParams{
			Context: ctx,
			ID:      int64(fileID),
		})
		if err != nil {
			logger.Warnf("Failed to get file deals for %v: %v", fileID, err)
			continue
		}

		// Make sure the file has a deal for every SP
		for _, deal := range getFileDealsRes.Payload {
			var foundDealForSP bool
			for _, sp := range s.storageProviders {
				if deal.Provider == sp.String() {
					foundDealForSP = true
					break
				}
			}

			if !foundDealForSP {
				// If no deal was found for this file and SP, the local bin file
				// cannot be deleted yet - continue to the next one
				continue binIteration
			}
		}

		// If deals have been made for all SPs, the local bin file can be
		// deleted
		binsToDelete = append(binsToDelete, binFileName)
	}

	for _, binFileName := range binsToDelete {
		if err := os.Remove(path.Join(s.local.Dir(), binFileName)); err != nil {
			logger.Warnf("Failed to delete local bin file %s that was staged for removal: %v", binFileName, err)
		}
	}
	logger.Infof("Cleaned up %v unneeded local files", len(binsToDelete))

	return nil
}
