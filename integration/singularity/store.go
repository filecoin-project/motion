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
	"sync"
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
	local      *blob.LocalStore
	sourceName string
	toPack     chan uint64
	closing    chan struct{}
	closed     sync.WaitGroup
}

func NewStore(o ...Option) (*SingularityStore, error) {
	opts, err := newOptions(o...)
	if err != nil {
		logger.Errorw("Failed to instantiate options", "err", err)
		return nil, err
	}
	return &SingularityStore{
		options:    opts,
		local:      blob.NewLocalStore(opts.storeDir),
		sourceName: "source",
		toPack:     make(chan uint64, 1),
		closing:    make(chan struct{}),
	}, nil
}

func (s *SingularityStore) initPreparation(ctx context.Context) (*models.ModelPreparation, error) {
	createSourceStorageRes, err := s.singularityClient.Storage.CreateLocalStorage(&storage.CreateLocalStorageParams{
		Context: ctx,
		Request: &models.StorageCreateLocalStorageRequest{
			Name: s.sourceName,
			Path: s.local.Dir(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create source storage: %w", err)
	}
	logger.Infow("Created source storage", "id", createSourceStorageRes.Payload.ID)

	createPreparationRes, err := s.singularityClient.Preparation.CreatePreparation(&preparation.CreatePreparationParams{
		Context: ctx,
		Request: &models.DataprepCreateRequest{
			MaxSize:        &s.maxCarSize,
			Name:           &s.preparationName,
			SourceStorages: []string{s.sourceName},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create preparation: %w", err)
	}
	logger.Infow("Created preparation", "id", createPreparationRes.Payload.ID)

	return createPreparationRes.Payload, nil
}

func (s *SingularityStore) Start(ctx context.Context) error {
	logger := logger.With("preparation", s.preparationName)

	// List out preparations and see if one with the configured name exists

	listPreparationsRes, err := s.singularityClient.Preparation.ListPreparations(&preparation.ListPreparationsParams{
		Context: ctx,
	})
	if err != nil {
		logger.Errorw("Failed to list preparations", "err", err)
		return fmt.Errorf("failed to list preparations: %w", err)
	}

	var preparation *models.ModelPreparation
	for _, preparationCmp := range listPreparationsRes.Payload {
		if preparationCmp.Name == s.preparationName {
			preparation = preparationCmp
			break
		}
	}
	if preparation == nil {
		// If no preparation was found, initialize it
		_, err = s.initPreparation(ctx)
		if err != nil {
			logger.Errorw("First-time preparation initialization failed", "err", err)
			return fmt.Errorf("first-time preparation initialization failed: %w", err)
		}
	}

	// Ensure default wallet is imported to singularity
	listWalletsRes, err := s.singularityClient.Wallet.ListWallets(&wallet.ListWalletsParams{
		Context: ctx,
	})
	if err != nil {
		logger.Errorw("Failed to list singularity wallets", "err", err)
		return fmt.Errorf("failed to list singularity wallets: %w", err)
	}
	var wlt *models.ModelWallet
	for _, existing := range listWalletsRes.Payload {
		if existing.PrivateKey == s.walletKey {
			wlt = existing
			logger.Infow("Wallet found on singularity", "id", existing.ID)
			break
		}
	}
	if wlt == nil {
		logger.Info("Wallet is not found on singularity. Importing...")
		importWalletRes, err := s.singularityClient.Wallet.ImportWallet(&wallet.ImportWalletParams{
			Context: ctx,
			Request: &models.WalletImportRequest{
				PrivateKey: s.walletKey,
			},
		})
		if err != nil {
			logger.Errorw("Failed to import wallet to singularity", "err", err)
			return fmt.Errorf("failed to import wallet: %w", err)
		}

		wlt = importWalletRes.Payload
	}

	// Ensure wallet is assigned to preparation
	listAttachedWalletsRes, err := s.singularityClient.WalletAssociation.ListAttachedWallets(&wallet_association.ListAttachedWalletsParams{
		Context: ctx,
		ID:      s.preparationName,
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
		if attachWalletRes, err := s.singularityClient.WalletAssociation.AttachWallet(&wallet_association.AttachWalletParams{
			Context: ctx,
			ID:      s.preparationName,
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
	listPreparationSchedulesRes, err := s.singularityClient.DealSchedule.ListPreparationSchedules(&deal_schedule.ListPreparationSchedulesParams{
		Context: ctx,
		ID:      s.preparationName,
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

	pricePerGBEpoch, _ := (new(big.Rat).SetFrac(s.pricePerGiBEpoch.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerGB, _ := (new(big.Rat).SetFrac(s.pricePerGiB.Int, big.NewInt(int64(1e18)))).Float64()
	pricePerDeal, _ := (new(big.Rat).SetFrac(s.pricePerDeal.Int, big.NewInt(int64(1e18)))).Float64()

	logger.Infof("Checking %v storage providers", len(s.storageProviders))
	for _, sp := range s.storageProviders {
		logger.Infof("Checking storage provider %s", sp)
		var foundSchedule *models.ModelSchedule
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
			_, err := s.singularityClient.DealSchedule.UpdateSchedule(&deal_schedule.UpdateScheduleParams{
				Context: ctx,
				ID:      foundSchedule.ID,
				Body: &models.ScheduleUpdateRequest{
					PricePerGbEpoch:       pricePerGBEpoch,
					PricePerGb:            pricePerGB,
					PricePerDeal:          pricePerDeal,
					Verified:              &s.verifiedDeal,
					Ipni:                  &s.ipniAnnounce,
					KeepUnsealed:          &s.keepUnsealed,
					StartDelay:            ptr.String(strconv.Itoa(int(s.dealStartDelay)*builtin.EpochDurationSeconds) + "s"),
					Duration:              ptr.String(strconv.Itoa(int(s.dealDuration)*builtin.EpochDurationSeconds) + "s"),
					ScheduleCron:          s.scheduleCron,
					ScheduleCronPerpetual: s.scheduleCronPerpetual,
					ScheduleDealNumber:    int64(s.scheduleDealNumber),
					TotalDealNumber:       int64(s.totalDealNumber),
					ScheduleDealSize:      s.scheduleDealSize,
					TotalDealSize:         s.totalDealSize,
					MaxPendingDealSize:    s.maxPendingDealSize,
					MaxPendingDealNumber:  int64(s.maxPendingDealNumber),
					URLTemplate:           s.scheduleUrlTemplate,
				},
			})
			if err != nil {
				logger.Errorw("Failed to update schedule for provider", "err", err)
				return fmt.Errorf("failed to update schedule: %w", err)
			}
		} else {
			// Otherwise, create it
			logger.Info("Schedule not found for provider. Creating...")
			if createScheduleRes, err := s.singularityClient.DealSchedule.CreateSchedule(&deal_schedule.CreateScheduleParams{
				Context: ctx,
				Schedule: &models.ScheduleCreateRequest{
					Preparation:           s.preparationName,
					Provider:              sp.String(),
					PricePerGbEpoch:       pricePerGBEpoch,
					PricePerGb:            pricePerGB,
					PricePerDeal:          pricePerDeal,
					Verified:              &s.verifiedDeal,
					Ipni:                  &s.ipniAnnounce,
					KeepUnsealed:          &s.keepUnsealed,
					StartDelay:            ptr.String(strconv.Itoa(int(s.dealStartDelay)*builtin.EpochDurationSeconds) + "s"),
					Duration:              ptr.String(strconv.Itoa(int(s.dealDuration)*builtin.EpochDurationSeconds) + "s"),
					ScheduleCron:          s.scheduleCron,
					ScheduleCronPerpetual: s.scheduleCronPerpetual,
					ScheduleDealNumber:    int64(s.scheduleDealNumber),
					TotalDealNumber:       int64(s.totalDealNumber),
					ScheduleDealSize:      s.scheduleDealSize,
					TotalDealSize:         s.totalDealSize,
					MaxPendingDealSize:    s.maxPendingDealSize,
					MaxPendingDealNumber:  int64(s.maxPendingDealNumber),
					URLTemplate:           s.scheduleUrlTemplate,
				},
			}); err != nil {
				logger.Errorw("Failed to create schedule for provider", "err", err)
				return fmt.Errorf("failed to create schedule: %w", err)
			} else {
				logger.Infow("Successfully created schedule for provider", "id", createScheduleRes.Payload.ID)
			}
		}
	}

	s.closed.Add(1)
	go s.runPreparationJobs()

	s.closed.Add(1)
	go s.runCleanupWorker()

	return nil
}

func (s *SingularityStore) runPreparationJobs() {
	defer s.closed.Done()

	// Create a context that gets canceled when this function exits.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		case <-s.closing:
			return
		case fileID := <-s.toPack:
			prepareToPackSourceRes, err := s.singularityClient.File.PrepareToPackFile(&file.PrepareToPackFileParams{
				Context: ctx,
				ID:      int64(fileID),
			})
			if err != nil {
				logger.Errorw("preparing to pack file", "fileID", fileID, "error", err)
			}
			if prepareToPackSourceRes.Payload > s.packThreshold {
				// mark outstanding pack jobs as ready to go so we can make CAR files
				_, err := s.singularityClient.Job.PrepareToPackSource(&job.PrepareToPackSourceParams{
					Context: ctx,
					ID:      s.preparationName,
					Name:    s.sourceName,
				})
				if err != nil {
					logger.Errorw("preparing to pack source", "error", err)
				}
			}
		}
	}
}

func (s *SingularityStore) Shutdown(ctx context.Context) error {
	close(s.closing)

	done := make(chan struct{})
	go func() {
		s.closed.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}

	logger.Infof("Singularity store shut down")

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
		size:   getFileRes.Payload.Size,
	}, nil
}

func (s *SingularityStore) Describe(ctx context.Context, id blob.ID) (*blob.Descriptor, error) {
	idStream, err := os.Open(path.Join(s.local.Dir(), id.String()+".id"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, blob.ErrBlobNotFound
		}

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
	descriptor := &blob.Descriptor{
		ID:               id,
		Size:             uint64(getFileRes.Payload.Size),
		ModificationTime: time.Unix(0, getFileRes.Payload.LastModifiedNano),
	}
	getFileDealsRes, err := s.singularityClient.File.GetFileDeals(&file.GetFileDealsParams{
		Context: ctx,
		ID:      int64(fileID),
	})
	if err != nil {
		return nil, err
	}

	if len(getFileDealsRes.Payload) == 0 {
		return descriptor, nil
	}

	replicas := make([]blob.Replica, 0, len(getFileDealsRes.Payload))
	for _, deal := range getFileDealsRes.Payload {
		updatedAt, err := time.Parse("2006-01-02 15:04:05-07:00", deal.LastVerifiedAt)
		if err != nil {
			updatedAt = time.Time{}
		}
		piece := blob.Piece{
			Expiration:  epochutil.EpochToTime(int32(deal.EndEpoch)),
			LastUpdated: updatedAt,
			PieceCID:    deal.PieceCid,
			Status:      string(deal.State),
		}
		replicas = append(replicas, blob.Replica{
			Provider: deal.Provider,
			Pieces:   []blob.Piece{piece},
		})
	}
	descriptor.Replicas = replicas
	return descriptor, nil
}

func (s *SingularityStore) runCleanupWorker() {
	defer s.closed.Done()

	// Run immediately once before starting ticker
	s.runCleanupJob()

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runCleanupJob()
		case <-s.closing:
			return
		}
	}
}

func (s *SingularityStore) runCleanupJob() {
	if err := s.cleanup(context.Background()); err != nil {
		logger.Errorf("Local store cleanup failed: %w", err)
	}
}

func (s *SingularityStore) cleanup(ctx context.Context) error {
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

		// Make sure the file has at least 1 deal for every SP
		for _, sp := range s.storageProviders {
			var foundDealForSP bool
			for _, deal := range getFileDealsRes.Payload {
				if deal.Provider == sp.String() && (deal.State == models.ModelDealStatePublished || deal.State == models.ModelDealStateActive) {
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
		logger.Infof("deleting local copy for deal %s, file %s", id, binFileName)
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
