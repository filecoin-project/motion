package replicationconfig

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
)

type (
	Option            func(*ReplicationConfig) error
	ReplicationConfig struct {
		StorageProviders []address.Address

		ReplicationFactor uint
		PricePerGiBEpoch  abi.TokenAmount
		PricePerGiB       abi.TokenAmount
		PricePerDeal      abi.TokenAmount
		DealStartDelay    abi.ChainEpoch
		DealDuration      abi.ChainEpoch
	}
)

func NewReplicationConfig(o ...Option) (*ReplicationConfig, error) {
	opts := &ReplicationConfig{
		DealStartDelay: builtin.EpochsInHour * 72,
		DealDuration:   builtin.EpochsInYear,
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.ReplicationFactor == 0 {
		// Default replication factor to the number of storage providers if zero.
		opts.ReplicationFactor = uint(len(opts.StorageProviders))
	}
	return opts, nil
}

// WithStorageProviders sets the list of Filecoin storage providers to make deals with.
// Defaults to no deals, i.e. local storage only if unspecified.
func WithStorageProviders(sp ...address.Address) Option {
	return func(o *ReplicationConfig) error {
		o.StorageProviders = sp
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
	return func(o *ReplicationConfig) error {
		o.ReplicationFactor = r
		return nil
	}
}

// WithPricePerGiBEpoch sets the price per epoch per GiB.
func WithPricePerGiBEpoch(v abi.TokenAmount) Option {
	return func(o *ReplicationConfig) error {
		o.PricePerGiBEpoch = v
		return nil
	}
}

// WithPricePerGiB sets the per epoch per GiB.
func WithPricePerGiB(v abi.TokenAmount) Option {
	return func(o *ReplicationConfig) error {
		o.PricePerGiB = v
		return nil
	}
}

// WithPricePerDeal sets the per deal.
func WithPricePerDeal(v abi.TokenAmount) Option {
	return func(o *ReplicationConfig) error {
		o.PricePerDeal = v
		return nil
	}
}

// WithDealStartDelay sets the delay for deal start epoch.
// Defaults to 72 hours if unspecified.
func WithDealStartDelay(v abi.ChainEpoch) Option {
	return func(o *ReplicationConfig) error {
		o.DealStartDelay = v
		return nil
	}
}

// WithDealDuration sets duration of Filecoin deals made.
// Defaults to one year if unspecified.
func WithDealDuration(v abi.ChainEpoch) Option {
	return func(o *ReplicationConfig) error {
		o.DealDuration = v
		return nil
	}
}
