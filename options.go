package motion

import (
	"os"

	"github.com/filecoin-project/go-address"
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
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{}
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
