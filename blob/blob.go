package blob

import (
	"context"
	"errors"
	"io"

	"github.com/ipfs/go-cid"
)

var (
	ErrBlobTooLarge = errors.New("blob size exceeds the maximum allowed")
	ErrBlobNotFound = errors.New("no blob is found with given ID")
)

type (
	ID         cid.Cid // TODO: Discuss if everyone is on board with using CIDs as blob ID.
	Descriptor struct {
		ID   ID // TODO: Discuss whether to use CIDs straight up.
		Size uint64
	}
	Store interface {
		Put(context.Context, io.ReadCloser) (*Descriptor, error)
		Get(context.Context, ID) (io.ReadSeekCloser, error)
	}
)

func (i *ID) String() string {
	return cid.Cid(*i).String()
}

func (i *ID) Decode(v string) error {
	decode, err := cid.Decode(v)
	if err != nil {
		return err
	}
	*i = ID(decode)
	return nil
}
