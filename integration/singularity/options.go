package singularity

import (
	"errors"
	"os"

	singularityclient "github.com/data-preservation-programs/singularity/client/swagger/http"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
)

type (
	// Option represents a configurable parameter in Motion service.
	Option  func(*options) error
	options struct {
		walletKey             string
		storeDir              string
		storageProviders      []address.Address
		replicationFactor     uint
		pricePerGiBEpoch      abi.TokenAmount
		pricePerGiB           abi.TokenAmount
		pricePerDeal          abi.TokenAmount
		dealStartDelay        abi.ChainEpoch
		dealDuration          abi.ChainEpoch
		maxCarSize            string
		packThreshold         int64
		preparationName       string
		singularityClient     *singularityclient.SingularityAPI
		scheduleUrlTemplate   string
		scheduleDealNumber    int
		scheduleCron          string
		scheduleCronPerpetual bool
		verifiedDeal          bool
		ipniAnnounce          bool
		keepUnsealed          bool
		totalDealNumber       int
		scheduleDealSize      string
		totalDealSize         string
		maxPendingDealSize    string
		maxPendingDealNumber  int
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		dealDuration:          builtin.EpochsInYear,
		dealStartDelay:        builtin.EpochsInHour * 72,
		maxCarSize:            "31.5GiB",
		packThreshold:         16 << 30,
		preparationName:       "MOTION_DATASET",
		scheduleCronPerpetual: true,
		verifiedDeal:          false,
		keepUnsealed:          true,
		ipniAnnounce:          true,
		scheduleDealSize:      "0",
		totalDealSize:         "0",
		maxPendingDealSize:    "0",
		maxPendingDealNumber:  0,
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.walletKey == "" {
		return nil, errors.New("must specify a wallet address")
	}
	if opts.storeDir == "" {
		opts.storeDir = os.TempDir()
	}
	if opts.replicationFactor == 0 {
		// Default replication factor to the number of storage providers if zero.
		opts.replicationFactor = uint(len(opts.storageProviders))
	}
	if opts.singularityClient == nil {
		opts.singularityClient = singularityclient.Default
	}
	return opts, nil
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

// WithStorageProviders sets the list of Filecoin storage providers to make deals with.
// Defaults to no deals, i.e. local storage only if unspecified.
func WithStorageProviders(sp ...address.Address) Option {
	return func(o *options) error {
		o.storageProviders = sp
		return nil
	}
}

// WithReplicationFactor sets the replication factor for the blobs.
// Defaults to the number of storage providers specified.
// If no storage providers are specified the replication factor will be zero,
// i.e. data will only be stored locally.
//
// See WithStorageProviders.
func WithReplicationFactor(r uint) Option {
	return func(o *options) error {
		o.replicationFactor = r
		return nil
	}
}

// WithPricePerGiBEpoch sets the price per epoch per GiB.
func WithPricePerGiBEpoch(v abi.TokenAmount) Option {
	return func(o *options) error {
		o.pricePerGiBEpoch = v
		return nil
	}
}

// WithPricePerGiB sets the per epoch per GiB.
func WithPricePerGiB(v abi.TokenAmount) Option {
	return func(o *options) error {
		o.pricePerGiB = v
		return nil
	}
}

// WithPricePerDeal sets the per deal.
func WithPricePerDeal(v abi.TokenAmount) Option {
	return func(o *options) error {
		o.pricePerDeal = v
		return nil
	}
}

// WithDealStartDelay sets the delay for deal start epoch.
// Defaults to 72 hours if unspecified.
func WithDealStartDelay(v abi.ChainEpoch) Option {
	return func(o *options) error {
		o.dealStartDelay = v
		return nil
	}
}

// WithDealDuration sets duration of Filecoin deals made.
// Defaults to one year if unspecified.
func WithDealDuration(v abi.ChainEpoch) Option {
	return func(o *options) error {
		o.dealDuration = v
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

// WithDatasetName sets the singularity dataset name used to store data.
// Defaults to "MOTION_DATASET".
func WithDatasetName(n string) Option {
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

// WithScheduleUrlTemplate sets the Singularity schedule URL template for online deals.
// Defaults to offline deals.
func WithScheduleUrlTemplate(t string) Option {
	return func(o *options) error {
		o.scheduleUrlTemplate = t
		return nil
	}
}

// WithScheduleDealNumber sets the max number of deals per singularity scheduled time.
// Defaults to unlimited.
func WithScheduleDealNumber(n int) Option {
	return func(o *options) error {
		o.scheduleDealNumber = n
		return nil
	}
}

// WithScheduleCron sets the Singularity schedule cron.
// Defaults to disabled.
func WithScheduleCron(c string) Option {
	return func(o *options) error {
		o.scheduleCron = c
		return nil
	}
}

// WithScheduleCronPerpetual sets whether a cron schedule should run in definitely.
// Defaults to true.
func WithScheduleCronPerpetual(v bool) Option {
	return func(o *options) error {
		o.scheduleCronPerpetual = v
		return nil
	}
}

// WithVerifiedDeal set whether the deals should be verified.
// Defaults to true.
func WithVerifiedDeal(v bool) Option {
	return func(o *options) error {
		o.verifiedDeal = v
		return nil
	}
}

// WithIPNIAnnounce set whether the deal payload should be announced to IPNI.
// Defaults to true.
func WithIPNIAnnounce(v bool) Option {
	return func(o *options) error {
		o.ipniAnnounce = v
		return nil
	}
}

// WithKeepUnsealed set whether the deal the deal should be kept unsealed.
// Defaults to false.
func WithKeepUnsealed(v bool) Option {
	return func(o *options) error {
		o.keepUnsealed = v
		return nil
	}
}

// WithTotalDealNumber sets the total number of deals.
// Defaults to 0, i.e. unlimited.
func WithTotalDealNumber(v int) Option {
	return func(o *options) error {
		o.totalDealNumber = v
		return nil
	}
}

// WithScheduleDealSize sets the size of deals per schedule trigger.
// Defaults to "0".
func WithScheduleDealSize(v string) Option {
	return func(o *options) error {
		o.scheduleDealSize = v
		return nil
	}
}

// WithTotalDealSize sets the total schedule deal size.
// Defaults to "0".
func WithTotalDealSize(v string) Option {
	return func(o *options) error {
		o.totalDealSize = v
		return nil
	}
}

// WithMaxPendingDealSize sets the max pending deal size.
// Defaults to "0".
func WithMaxPendingDealSize(v string) Option {
	return func(o *options) error {
		o.maxPendingDealSize = v
		return nil
	}
}

// WithMaxPendingDealNumber sets the max pending deal number.
// Defaults to 1.
func WithMaxPendingDealNumber(v int) Option {
	return func(o *options) error {
		o.maxPendingDealNumber = v
		return nil
	}
}
