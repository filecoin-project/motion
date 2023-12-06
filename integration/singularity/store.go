package singularity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/data-preservation-programs/singularity/client/swagger/http/admin"
	"github.com/data-preservation-programs/singularity/client/swagger/http/deal_schedule"
	"github.com/data-preservation-programs/singularity/client/swagger/http/file"
	"github.com/data-preservation-programs/singularity/client/swagger/http/job"
	"github.com/data-preservation-programs/singularity/client/swagger/http/preparation"
	"github.com/data-preservation-programs/singularity/client/swagger/http/storage"
	"github.com/data-preservation-programs/singularity/client/swagger/http/wallet"
	"github.com/data-preservation-programs/singularity/client/swagger/http/wallet_association"
	"github.com/data-preservation-programs/singularity/client/swagger/models"
	"github.com/data-preservation-programs/singularity/service/epochutil"
	"github.com/filecoin-project/motion/blob"
	"github.com/gotidy/ptr"
	"github.com/ipfs/go-log/v2"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v3"
)

var logger = log.Logger("motion/integration/singularity")

type Store struct {
	*options
	local            *blob.LocalStore
	idMap            *idMap
	cleanupScheduler *cleanupScheduler
	sourceName       string
	toPack           chan uint64
	closing          chan struct{}
	closed           sync.WaitGroup
	forcePack        *time.Ticker
	spConfig         map[string]ProviderConfig
}

type ProviderConfig struct {
	ContentProviderDomain *string  `yaml:"content_provider_domain"`
	ScheduleCron          *string  `yaml:"schedule_cron"`
	ScheduleDealCount     *int64   `yaml:"schedule_deal_count"`
	VerifiedDeal          *bool    `yaml:"verified_deal"`
	PricePerGibEpoch      *float64 `yaml:"price_per_gib_epoch"`
	PricePerGib           *float64 `yaml:"price_per_gib"`
	PricePerDeal          *float64 `yaml:"price_per_deal"`
	DealStartDelay        *string  `yaml:"deal_start_delay"`
	DealDuration          *string  `yaml:"deal_duration"`
}

func (c *ProviderConfig) applyDefault(def ProviderConfig) {
	if c.ContentProviderDomain == nil {
		c.ContentProviderDomain = def.ContentProviderDomain
	}
	if c.ScheduleCron == nil {
		c.ScheduleCron = def.ScheduleCron
	}
	if c.VerifiedDeal == nil {
		c.VerifiedDeal = def.VerifiedDeal
	}
	if c.PricePerGibEpoch == nil {
		c.PricePerGibEpoch = def.PricePerGibEpoch
	}
	if c.PricePerGib == nil {
		c.PricePerGib = def.PricePerGib
	}
	if c.PricePerDeal == nil {
		c.PricePerDeal = def.PricePerDeal
	}
	if c.DealStartDelay == nil {
		c.DealStartDelay = def.DealStartDelay
	}
	if c.DealDuration == nil {
		c.DealDuration = def.DealDuration
	}
	if c.ScheduleDealCount == nil {
		c.ScheduleDealCount = def.ScheduleDealCount
	}
}

var defaultProviderConfig = ProviderConfig{
	ContentProviderDomain: ptr.Of(""),
	ScheduleCron:          ptr.Of("* * * * *"),
	VerifiedDeal:          ptr.Of(false),
	PricePerGibEpoch:      ptr.Of(0.0),
	PricePerGib:           ptr.Of(0.0),
	PricePerDeal:          ptr.Of(0.0),
	DealStartDelay:        ptr.Of("72h"),
	DealDuration:          ptr.Of("8760h"),
	ScheduleDealCount:     ptr.Of(int64(1)),
}

func NewStore(o ...Option) (*Store, error) {
	opts, err := newOptions(o...)
	if err != nil {
		return nil, fmt.Errorf("failed to init options: %w", err)
	}

	cleanupSchedulerCfg := cleanupSchedulerConfig{
		interval: opts.cleanupInterval,
	}

	store := &Store{
		options:    opts,
		local:      blob.NewLocalStore(opts.storeDir, blob.WithMinFreeSpace(opts.minFreeSpace)),
		idMap:      newIDMap(opts.storeDir),
		sourceName: "source",
		toPack:     make(chan uint64, 1),
		closing:    make(chan struct{}),
		forcePack:  time.NewTicker(opts.forcePackAfter),
	}

	store.cleanupScheduler = newCleanupScheduler(cleanupSchedulerCfg, store.local, store.hasDealForAllProviders)

	return store, nil
}

func (s *Store) initPreparation(ctx context.Context) (*models.ModelPreparation, error) {
	createSourceStorageRes, err := s.singularityClient.Storage.CreateLocalStorage(&storage.CreateLocalStorageParams{
		Context: ctx,
		Request: &models.StorageCreateLocalStorageRequest{
			Name: s.sourceName,
			Path: s.local.Dir(),
		},
	})
	if err == nil && !createSourceStorageRes.IsSuccess() {
		err = errors.New(createSourceStorageRes.Error())
	}
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

func (s *Store) processSPConfig(ctx context.Context) error {
	logger.Info("Processing SP config from config dir")
	configStr, err := os.ReadFile(filepath.Join(s.configDir, "sps.yml"))
	if err != nil {
		logger.Errorw("Failed to read SP config. Expect sps.yml in config dir.", "err", err)
	}

	configMap := make(map[string]ProviderConfig)
	err = yaml.Unmarshal(configStr, &configMap)
	if err != nil {
		return fmt.Errorf("failed to parse SP config: %w", err)
	}

	newConfig := make(map[string]ProviderConfig)

	def, ok := configMap["default"]
	if ok {
		def.applyDefault(defaultProviderConfig)
	} else {
		def = defaultProviderConfig
	}
	for sp, cfg := range configMap {
		if sp != "default" {
			cfg.applyDefault(def)
			newConfig[sp] = cfg
		}
	}
	s.spConfig = newConfig
	return s.applySPConfigChange(ctx)
}

func (s *Store) applySPConfigChange(ctx context.Context) error {
	schedules, err := s.singularityClient.DealSchedule.ListPreparationSchedules(&deal_schedule.ListPreparationSchedulesParams{
		Context: ctx,
		ID:      s.preparationName,
	})
	if err != nil {
		return fmt.Errorf("failed to list schedules for preparation: %w", err)
	}
	// Delete all schedules that are not in the new config
	for _, schd := range schedules.Payload {
		if _, ok := s.spConfig[schd.Provider]; !ok {
			logger.Infow("Deleting schedule", "provider", schd.Provider)
			_, err := s.singularityClient.DealSchedule.RemoveSchedule(&deal_schedule.RemoveScheduleParams{
				Context: ctx,
				ID:      schd.ID,
			})
			if err != nil {
				return fmt.Errorf("failed to delete schedule for provider: %w", err)
			}
		}
	}
	// Create new schedules for new providers in the new config, or update schedules for existing providers
	for sp, cfg := range s.spConfig {
		found := slices.IndexFunc(schedules.Payload, func(s *models.ModelSchedule) bool {
			return s.Provider == sp
		})
		if found == -1 {
			logger.Infow("Creating schedule", "provider", sp)
			_, err := s.singularityClient.DealSchedule.CreateSchedule(&deal_schedule.CreateScheduleParams{
				Schedule: &models.ScheduleCreateRequest{
					Duration:           cfg.DealDuration,
					Preparation:        s.preparationName,
					PricePerDeal:       *cfg.PricePerDeal,
					PricePerGb:         *cfg.PricePerGib,
					PricePerGbEpoch:    *cfg.PricePerGibEpoch,
					Provider:           sp,
					ScheduleCron:       *cfg.ScheduleCron,
					StartDelay:         cfg.DealStartDelay,
					URLTemplate:        *cfg.ContentProviderDomain,
					Verified:           cfg.VerifiedDeal,
					ScheduleDealNumber: *cfg.ScheduleDealCount,
				},
				Context: ctx,
			})
			if err != nil {
				return fmt.Errorf("failed to create schedule for provider: %w", err)
			}
		} else {
			logger.Infow("Updating schedule", "provider", sp)
			current := schedules.Payload[found]
			_, err := s.singularityClient.DealSchedule.UpdateSchedule(&deal_schedule.UpdateScheduleParams{
				Body: &models.ScheduleUpdateRequest{
					Duration:           cfg.DealDuration,
					PricePerDeal:       *cfg.PricePerDeal,
					PricePerGb:         *cfg.PricePerGib,
					PricePerGbEpoch:    *cfg.PricePerGibEpoch,
					ScheduleCron:       *cfg.ScheduleCron,
					StartDelay:         cfg.DealStartDelay,
					URLTemplate:        *cfg.ContentProviderDomain,
					Verified:           cfg.VerifiedDeal,
					ScheduleDealNumber: *cfg.ScheduleDealCount,
				},
				ID:      current.ID,
				Context: ctx,
			})
			if err != nil {
				return fmt.Errorf("failed to update schedule for provider: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) Start(ctx context.Context) error {
	logger := logger.With("preparation", s.preparationName)

	// Set the identity to Motion for tracking purpose
	_, err := s.singularityClient.Admin.SetIdentity(&admin.SetIdentityParams{
		Context: ctx,
		Request: &models.AdminSetIdentityRequest{
			Identity: "Motion",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to set motion identity: %w (are you using Singularity v0.5.4+?)", err)
	}

	// List out preparations and see if one with the configured name exists
	listPreparationsRes, err := s.singularityClient.Preparation.ListPreparations(&preparation.ListPreparationsParams{
		Context: ctx,
	})
	if err != nil {
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
			return fmt.Errorf("first-time preparation initialization failed: %w", err)
		}
	}

	// Ensure default wallet is imported to singularity
	listWalletsRes, err := s.singularityClient.Wallet.ListWallets(&wallet.ListWalletsParams{
		Context: ctx,
	})
	if err != nil {
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
		logger.Info("Wallet is not found on singularity. Importing wallet")
		importWalletRes, err := s.singularityClient.Wallet.ImportWallet(&wallet.ImportWalletParams{
			Context: ctx,
			Request: &models.WalletImportRequest{
				PrivateKey: s.walletKey,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to import wallet to singularity: %w", err)
		}

		wlt = importWalletRes.Payload
	}

	// Ensure wallet is assigned to preparation
	listAttachedWalletsRes, err := s.singularityClient.WalletAssociation.ListAttachedWallets(&wallet_association.ListAttachedWalletsParams{
		Context: ctx,
		ID:      s.preparationName,
	})
	if err != nil {
		return fmt.Errorf("failed to list attached wallets: %w", err)
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
		logger.Info("Wallet was not found. Creating wallet")
		if attachWalletRes, err := s.singularityClient.WalletAssociation.AttachWallet(&wallet_association.AttachWalletParams{
			Context: ctx,
			ID:      s.preparationName,
			Wallet:  wlt.Address,
		}); err != nil {
			return fmt.Errorf("failed to add wallet to preparation: %w", err)
		} else {
			logger.Infow("Successfully added wallet to preparation", "id", attachWalletRes.Payload.ID)
		}
	}

	// Start a goroutine to periodically check for SP config changes
	go func() {
		for {
			err = s.processSPConfig(ctx)
			if err != nil {
				logger.Errorw("Failed to process SP config", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		}
	}()

	s.cleanupScheduler.start(ctx)

	s.closed.Add(1)
	go s.runPreparationJobs()

	return nil
}

func (s *Store) runPreparationJobs() {
	defer s.closed.Done()

	// Create a context that gets canceled when this function exits.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		// If context is cancelled, end
		case <-s.closing:
			return

		// If a new file came in, prepare it for packing, and mark the source
		// ready to pack if the threshold is reached. Also reset the timer.
		case fileID := <-s.toPack:
			prepareToPackFileRes, err := s.singularityClient.File.PrepareToPackFile(&file.PrepareToPackFileParams{
				Context: ctx,
				ID:      int64(fileID),
			})
			if err != nil {
				logger.Errorw("Failed to prepare to pack file", "fileID", fileID, "error", err)
				continue
			}
			logger.Infow("Prepared file for packing", "fileID", fileID)
			if prepareToPackFileRes.Payload > s.packThreshold {
				if err := s.prepareToPackSource(ctx); err != nil {
					logger.Errorw("Failed to prepare to pack source", "error", err)
					continue
				}
			}
			s.resetForcePackTimer()

		// If forced pack message comes through (e.g. from pack threshold max
		// wait time being exceeded), prepare to pack source immediately
		case <-s.forcePack.C:
			logger.Infof("Pack threshold not met after max wait time of %s, forcing pack of any pending data", s.forcePackAfter)
			if err := s.prepareToPackSource(ctx); err != nil {
				logger.Errorw("Failed to prepare to pack source (forced)", "error", err)
				continue
			}
		}
	}
}

// Marks outstanding pack jobs as ready to go so CAR files can be made, and
// updates the last pack time
func (s *Store) prepareToPackSource(ctx context.Context) error {
	_, err := s.singularityClient.Job.PrepareToPackSource(&job.PrepareToPackSourceParams{
		Context: ctx,
		ID:      s.preparationName,
		Name:    s.sourceName,
	})

	s.resetForcePackTimer()

	return err
}

func (s *Store) resetForcePackTimer() {
	s.forcePack.Reset(s.forcePackAfter)
}

func (s *Store) Shutdown(ctx context.Context) error {
	close(s.closing)

	done := make(chan struct{})
	go func() {
		s.closed.Wait()
		close(done)
	}()

	s.cleanupScheduler.stop(ctx)

	select {
	case <-ctx.Done():
	case <-done:
	}

	s.forcePack.Stop()

	logger.Info("Singularity store shut down")

	return nil
}

func (s *Store) Put(ctx context.Context, reader io.Reader) (*blob.Descriptor, error) {
	desc, err := s.local.Put(ctx, reader)
	if err != nil {
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
		return nil, fmt.Errorf("error creating singularity entry at %s: %w", filePath, err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.toPack <- uint64(pushFileRes.Payload.ID):
	}

	s.idMap.insert(desc.ID, pushFileRes.Payload.ID)

	logger.Infow("Stored blob successfully", "id", desc.ID.String(), "size", desc.Size, "singularityFileID", pushFileRes.Payload.ID)

	return desc, nil
}

func (s *Store) PassGet(w http.ResponseWriter, r *http.Request, id blob.ID) {

	fileID, err := s.idMap.get(id)
	if err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			http.Error(w, "", http.StatusNotFound)
			return
		}

		logger.Errorw("Could not get singularity file ID", "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	params := &file.RetrieveFileParams{
		Context: r.Context(),
		ID:      int64(fileID),
	}
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		params.Range = ptr.String(rangeHeader)
	}

	_, _, err = s.singularityClient.File.RetrieveFile(params, w)
	if err != nil {
		logger.Errorw("Failed to retrieve file slice", "err", err, "id", id.String(), "fileID", fileID)
		if strings.Contains(err.Error(), "404") {
			http.Error(w, "", http.StatusNotFound)
			return
		}
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	logger.Infow("Retrieved file", "id", id.String())
}

func (s *Store) Get(ctx context.Context, id blob.ID) (io.ReadSeekCloser, error) {
	fileID, err := s.idMap.get(id)
	if err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			return nil, blob.ErrBlobNotFound
		}
		return nil, fmt.Errorf("could not get singularity file ID: %w", err)
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

	return NewReader(s.singularityClient, uint64(fileID), getFileRes.Payload.Size), nil
}

func (s *Store) Describe(ctx context.Context, id blob.ID) (*blob.Descriptor, error) {
	fileID, err := s.idMap.get(id)
	if err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			return nil, blob.ErrBlobNotFound
		}
		return nil, fmt.Errorf("could not get Singularity file ID: %w", err)
	}

	getFileRes, err := s.singularityClient.File.GetFile(&file.GetFileParams{
		Context: ctx,
		ID:      int64(fileID),
	})
	if err != nil {
		// TODO(@elijaharita): this is not very robust, but is there even a better way?
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

// Returns true if the file has at least 1 deal for every SP.
func (s *Store) hasDealForAllProviders(ctx context.Context, blobID blob.ID) (bool, error) {
	fileID, err := s.idMap.get(blobID)
	if err != nil {
		return false, fmt.Errorf("could not get Singularity file ID: %w", err)
	}

	getFileDealsRes, err := s.singularityClient.File.GetFileDeals(&file.GetFileDealsParams{
		Context: ctx,
		ID:      fileID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to get file deals: %w", err)
	}

	// Make sure the file has at least 1 deal for every SP
	for sp, _ := range s.spConfig {
		foundDealForSP := false
		for _, deal := range getFileDealsRes.Payload {
			// Only check state for current provider
			if deal.Provider != sp {
				continue
			}

			if deal.State == models.ModelDealStatePublished || deal.State == models.ModelDealStateActive {
				foundDealForSP = true
				break
			}
		}
		if !foundDealForSP {
			return false, nil
		}
	}

	return true, nil
}
