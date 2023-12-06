package singularity

import (
	"errors"
	"fmt"
	"os"
	"time"

	singularityclient "github.com/data-preservation-programs/singularity/client/swagger/http"
)

type (
	// Option represents a configurable parameter in Motion service.
	Option  func(*options) error
	options struct {
		walletKey         string
		storeDir          string
		configDir         string
		maxCarSize        string
		packThreshold     int64
		forcePackAfter    time.Duration
		preparationName   string
		singularityClient *singularityclient.SingularityAPI
		cleanupInterval   time.Duration
		minFreeSpace      int64
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		maxCarSize:      "31.5GiB",
		packThreshold:   16 << 30,
		forcePackAfter:  time.Hour * 24,
		preparationName: "MOTION_PREPARATION",
		cleanupInterval: time.Hour,
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}
	if opts.walletKey == "" {
		return nil, errors.New("must specify a wallet address")
	}
	if opts.storeDir == "" {
		opts.storeDir = os.TempDir()
	}
	if opts.singularityClient == nil {
		opts.singularityClient = singularityclient.Default
	}
	return opts, nil
}

// WithConfigDir sets local directory used by the singularity store.
// Defaults to current directory.
func WithConfigDir(s string) Option {
	return func(o *options) error {
		o.configDir = s
		return nil
	}
}

// WithStoreDir sets local directory used by the singularity store.
// Defaults to OS temporary directory.
// See: os.TempDir.
func WithStoreDir(s string) Option {
	return func(o *options) error {
		o.storeDir = s
		return nil
	}
}

// WithWalletKey sets the wallet used by Motion to interact with Filecoin network.
func WithWalletKey(wk string) Option {
	return func(o *options) error {
		o.walletKey = wk
		return nil
	}
}

// WithMaxCarSize sets singularity max car size config as string.
// Defaults to "31.5GiB"
func WithMaxCarSize(s string) Option {
	return func(o *options) error {
		o.maxCarSize = s
		return nil
	}
}

// WithPackThreshold sets the threshold at which unpacked bytes are scheduled for packing.
// Defaults to 16 GiB.
func WithPackThreshold(s int64) Option {
	return func(o *options) error {
		o.packThreshold = s
		return nil
	}
}

// WithForcePackAfter sets the maximum amount of time to wait without any data being received before forcing packing.
// Defaults to 24 hours.
func WithForcePackAfter(d time.Duration) Option {
	return func(o *options) error {
		o.forcePackAfter = d
		return nil
	}
}

// WithPreparationName sets the singularity preparation name used to store data.
// Defaults to "MOTION_PREPARATION".
func WithPreparationName(n string) Option {
	return func(o *options) error {
		o.preparationName = n
		return nil
	}
}

// WithSingularityClient sets the client used to communicate with Singularity API.
// Defaults to HTTP client with API endpoint http://localhost:9090.
func WithSingularityClient(c *singularityclient.SingularityAPI) Option {
	return func(o *options) error {
		o.singularityClient = c
		return nil
	}
}

// WithCleanupInterval sets how often to check for and remove data that has been successfully stored on Filecoin.
// Deafults to time.Hour
func WithCleanupInterval(v time.Duration) Option {
	return func(o *options) error {
		o.cleanupInterval = v
		return nil
	}
}

// WithMinFreeSpce configures the minimul free disk space that must remain
// after storing a blob. A value of zero uses the default value and -1 disabled
// checks.
func WithMinFreeSpace(space int64) Option {
	return func(o *options) error {
		o.minFreeSpace = space
		return nil
	}
}
