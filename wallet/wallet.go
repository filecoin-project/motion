package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

var (
	_ wallet.Default = (*Wallet)(nil)
	_ api.Wallet     = (*Wallet)(nil)
)

type Wallet struct {
	*options
	ks    types.KeyStore
	local *wallet.LocalWallet
}

func New(o ...Option) (*Wallet, error) {
	opts, err := newOptions(o...)
	if err != nil {
		return nil, err
	}
	ks, err := opts.keyStoreOpener()
	if err != nil {
		return nil, err
	}
	localWallet, err := opts.localWalletOpener(ks)
	if err != nil {
		return nil, err
	}
	switch list, err := localWallet.WalletList(context.TODO()); {
	case err != nil:
		return nil, fmt.Errorf("failed to list wallet: %w", err)
	case len(list) == 0:
		if !opts.generateKeyIfNotExist {
			return nil, errors.New("wallet must contain at least one key")
		}
		addr, err := localWallet.WalletNew(context.TODO(), opts.defaultKeyType)
		if err != nil {
			return nil, fmt.Errorf("failed to generate wallet key: %w", err)
		}
		logger.Infow("Generated a new wallet key", "address", addr)
		logger.Warn("Please make sure to backup the newly generated wallet key to avoid loss.")
		logger.Warn("Please make sure the wallet has enough funds before using Motion.")
	default:
		logger.Infof("Found %d wallet addresses", len(list))
		logger.Infow("Wallet addresses", "addresses", list)
	}
	w := &Wallet{
		options: opts,
		ks:      ks,
		local:   localWallet,
	}
	return w, nil
}

func (w *Wallet) WalletNew(ctx context.Context, keyType types.KeyType) (address.Address, error) {
	return w.local.WalletNew(ctx, keyType)
}

func (w *Wallet) WalletHas(ctx context.Context, a address.Address) (bool, error) {
	return w.local.WalletHas(ctx, a)
}

func (w *Wallet) WalletList(ctx context.Context) ([]address.Address, error) {
	return w.local.WalletList(ctx)
}

func (w *Wallet) WalletSign(ctx context.Context, signer address.Address, toSign []byte, meta api.MsgMeta) (*crypto.Signature, error) {
	return w.local.WalletSign(ctx, signer, toSign, meta)
}

func (w *Wallet) WalletExport(ctx context.Context, a address.Address) (*types.KeyInfo, error) {
	return w.local.WalletExport(ctx, a)
}

func (w *Wallet) WalletImport(ctx context.Context, info *types.KeyInfo) (address.Address, error) {
	return w.local.WalletImport(ctx, info)
}

func (w *Wallet) WalletDelete(ctx context.Context, a address.Address) error {
	return w.local.WalletDelete(ctx, a)
}

func (w *Wallet) GetDefault() (address.Address, error) {
	switch addr, err := w.local.GetDefault(); {
	case err == nil:
		return addr, nil
	case errors.Is(err, types.ErrKeyInfoNotFound):
		logger.Infow("No default wallet address is set. Falling back on the first address in wallet...")
		wl, err := w.WalletList(context.Background())
		if err != nil {
			return address.Address{}, err
		}
		if len(wl) == 0 {
			return address.Address{}, fmt.Errorf("wallet has no keys: %w", types.ErrKeyInfoNotFound)
		}
		// Pick the first address as the new default and set it as default for future interactions.
		addr = wl[0]
		if err := w.SetDefault(addr); err != nil {
			// We could allow the execution to continue; but return error just to be on the conservative side.
			// This avoids inconsistent behaviour in case the next call to WalletList returns a different list
			// of addresses, which can result in all sorts of bad time in terms of signed deals.
			return address.Address{}, fmt.Errorf("failed to set default wallet address to the first wallet found: %w", err)
		}
		logger.Infow("Default wallet address is now set to the first address in wallet", "address", addr)
		return addr, nil
	default:
		return address.Address{}, err
	}
}

func (w *Wallet) SetDefault(a address.Address) error {
	return w.local.SetDefault(a)
}
