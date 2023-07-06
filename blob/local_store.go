package blob

import (
	"context"
	"errors"
	"io"
	"os"
	"path"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

var _ Store = (*LocalStore)(nil)

type LocalStore struct {
	dir string
}

func NewLocalStore(dir string) *LocalStore {
	return &LocalStore{
		dir: dir,
	}
}

func (l *LocalStore) Put(_ context.Context, reader io.ReadCloser) (*Descriptor, error) {
	hasher, err := multihash.GetHasher(multihash.SHA2_256)
	if err != nil {
		return nil, err
	}
	teeReader := io.TeeReader(reader, hasher)
	dest, err := os.CreateTemp("", "motion_local_store_*.bin")
	if err != nil {
		return nil, err
	}
	defer dest.Close()
	written, err := io.Copy(dest, teeReader)
	if err != nil {
		return nil, err
	}
	sum := hasher.Sum(nil)
	mh, err := multihash.Encode(sum, multihash.SHA2_256)
	if err != nil {
		return nil, err
	}
	id := cid.NewCidV1(cid.Raw, mh)
	if err = os.Rename(dest.Name(), path.Join(l.dir, id.String()+".bin")); err != nil {
		return nil, err
	}
	return &Descriptor{
		ID:   ID(id),
		Size: uint64(written),
	}, nil
}

func (l *LocalStore) Get(_ context.Context, id ID) (io.ReadSeekCloser, error) {
	switch blob, err := os.Open(path.Join(l.dir, id.String()+".bin")); {
	case err == nil:
		return blob, nil
	case errors.Is(err, os.ErrNotExist):
		return nil, ErrBlobNotFound
	default:
		return nil, err
	}
}
