package motion

import (
	"os"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/motion/api/server"
	"github.com/filecoin-project/motion/blob"
	"github.com/filecoin-project/motion/wallet"
)

type (
	// Option represents a configurable parameter in Motion service.
	Option  func(*options) error
	options struct {
		serverOptions    []server.Option
		blobStore        blob.Store
		wallet           *wallet.Wallet
		storageProviders []address.Address

		replicationFactor uint
		pricePerGiBEpoch  abi.TokenAmount
		pricePerGiB       abi.TokenAmount
		pricePerDeal      abi.TokenAmount
		dealStartDelay    abi.ChainEpoch
		dealDuration      abi.ChainEpoch
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		dealStartDelay: builtin.EpochsInHour * 72,
		dealDuration:   builtin.EpochsInYear,
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.blobStore == nil {
		dir := os.TempDir()
		logger.Warnw("No blob store is specified. Falling back on local blob store in temporary directory.", "dir", dir)
		opts.blobStore = blob.NewLocalStore(dir)
	}
	if opts.replicationFactor == 0 {
		// Default replication factor to the number of storage providers if zero.
		opts.replicationFactor = uint(len(opts.storageProviders))
	}
	return opts, nil
}

// WithServerOptions sets the options to be used when instantiating server.HttpServer.
// Defaults to no options.
func WithServerOptions(serverOptions ...server.Option) Option {
	return func(o *options) error {
		o.serverOptions = serverOptions
		return nil
	}
}

// WithBlobStore sets the blob.Store to use for storage and retrieval of blobs.
// Defaults to blob.LocalStore at a temporary directory.
// See: blob.NewLocalStore, os.TempDir.
func WithBlobStore(s blob.Store) Option {
	return func(o *options) error {
		o.blobStore = s
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
