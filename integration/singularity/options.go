package singularity

import (
	"net/http"
	"os"

	"github.com/data-preservation-programs/singularity/client"
	httpclient "github.com/data-preservation-programs/singularity/client/http"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/motion/wallet"
)

type (
	// Option represents a configurable parameter in Motion service.
	Option  func(*options) error
	options struct {
		wallet              *wallet.Wallet
		storeDir            string
		storageProviders    []address.Address
		replicationFactor   uint
		pricePerGiBEpoch    abi.TokenAmount
		pricePerGiB         abi.TokenAmount
		pricePerDeal        abi.TokenAmount
		dealStartDelay      abi.ChainEpoch
		dealDuration        abi.ChainEpoch
		maxCarSize          string
		packThreshold       int64
		datasetName         string
		singularityClient   client.Client
		scheduleUrlTemplate string
		scheduleDealNumber  int
		scheduleCron        string
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		dealDuration:   builtin.EpochsInYear,
		dealStartDelay: builtin.EpochsInHour * 72,
		maxCarSize:     "31.5GiB",
		packThreshold:  16 << 30,
		datasetName:    "MOTION_DATASET",
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.wallet == nil {
		var err error
		opts.wallet, err = wallet.New()
		return nil, err
	}
	if opts.storeDir == "" {
		opts.storeDir = os.TempDir()
	}
	if opts.replicationFactor == 0 {
		// Default replication factor to the number of storage providers if zero.
		opts.replicationFactor = uint(len(opts.storageProviders))
	}
	if opts.singularityClient == nil {
		opts.singularityClient = httpclient.NewHTTPClient(http.DefaultClient, "http://localhost:9090")
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

// WithWallet sets the wallet used by Motion to interact with Filecoin network.
// Defaults to wallet.New.
func WithWallet(w *wallet.Wallet) Option {
	return func(o *options) error {
		o.wallet = w
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
		o.datasetName = n
		return nil
	}
}

// WithSingularityClient sets the client used to communicate with Singularity API.
// Defaults to HTTP client with API endpoint http://localhost:9090.
func WithSingularityClient(c client.Client) Option {
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
