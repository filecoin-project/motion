package wallet

import (
	"errors"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

type (
	// Option represents a configurable parameter in Motion service.
	Option  func(*options) error
	options struct {
		keyStoreOpener        func() (types.KeyStore, error)
		localWalletOpener     func(ks types.KeyStore) (*wallet.LocalWallet, error)
		generateKeyIfNotExist bool
		defaultKeyType        types.KeyType
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		keyStoreOpener:        DefaultDiskKeyStoreOpener("", true),
		localWalletOpener:     wallet.NewWallet,
		generateKeyIfNotExist: true,
		defaultKeyType:        types.KTSecp256k1,
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.keyStoreOpener == nil {
		return nil, errors.New("keystore opener must be specified")
	}
	return opts, nil
}

func WithKeyStoreOpener(opener func() (types.KeyStore, error)) Option {
	return func(o *options) error {
		o.keyStoreOpener = opener
		return nil
	}
}

func WithGenerateKeyIfNotExist(b bool) Option {
	return func(o *options) error {
		o.generateKeyIfNotExist = b
		return nil
	}
}

func WithDefaultKeyType(t types.KeyType) Option {
	return func(o *options) error {
		o.defaultKeyType = t
		return nil
	}
}
