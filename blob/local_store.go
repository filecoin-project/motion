package blob

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
)

var _ Store = (*LocalStore)(nil)

// LocalStore is a Store that stores blobs as flat files in a configured directory.
// Blobs are stored as flat files, named by their ID with .bin extension.
// This store is used primarily for testing purposes.
type LocalStore struct {
	dir string
}

// NewLocalStore instantiates a new LocalStore and uses the given dir as the place to store blobs.
// Blobs are stored as flat files, named by their ID with .bin extension.
func NewLocalStore(dir string) *LocalStore {
	return &LocalStore{
		dir: dir,
	}
}

// Put reads the given reader fully and stores its content in the store directory as flat files.
// The reader content is first stored in a temporary directory and upon successful storage is moved to the store directory.
// The Descriptor.ModificationTime is set to the modification date of the file that corresponds to the content.
// The Descriptor.ID is randomly generated; see NewID.
func (l *LocalStore) Put(_ context.Context, reader io.ReadCloser) (*Descriptor, error) {
	id, err := NewID()
	if err != nil {
		return nil, err
	}
	dest, err := os.CreateTemp("", "motion_local_store_*.bin")
	if err != nil {
		return nil, err
	}
	defer dest.Close()
	written, err := io.Copy(dest, reader)
	if err != nil {
		os.Remove(dest.Name())
		return nil, err
	}
	if err = os.Rename(dest.Name(), path.Join(l.dir, id.String()+".bin")); err != nil {
		return nil, err
	}
	stat, err := dest.Stat()
	if err != nil {
		return nil, err
	}
	return &Descriptor{
		ID:               *id,
		Size:             uint64(written),
		ModificationTime: stat.ModTime(),
	}, nil
}

// Get Retrieves the content of blob log with its Descriptor.
// If no file is found for the given id, ErrBlobNotFound is returned.
func (l *LocalStore) Get(_ context.Context, id ID) (io.ReadSeekCloser, *Descriptor, error) {
	switch blob, err := os.Open(path.Join(l.dir, id.String()+".bin")); {
	case err == nil:
		stat, err := blob.Stat()
		if err != nil {
			return nil, nil, err
		}
		return blob, &Descriptor{
			ID:               id,
			Size:             uint64(stat.Size()),
			ModificationTime: stat.ModTime(),
		}, nil
	case errors.Is(err, os.ErrNotExist):
		return nil, nil, ErrBlobNotFound
	default:
		return nil, nil, err
	}
}
