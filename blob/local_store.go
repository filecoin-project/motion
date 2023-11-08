package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gammazero/fsutil/disk"
)

var _ Store = (*LocalStore)(nil)

const (
	Kib = 1 << (10 * (iota + 1))
	Mib
	Gib
)

const defaultMinFreeSpace = 64 * Mib

// LocalStore is a Store that stores blobs as flat files in a configured directory.
// Blobs are stored as flat files, named by their ID with .bin extension.
// This store is used primarily for testing purposes.
type LocalStore struct {
	dir          string
	minFreeSpace uint64
}

// NewLocalStore instantiates a new LocalStore and uses the given dir as the place to store blobs.
// Blobs are stored as flat files, named by their ID with .bin extension.
func NewLocalStore(dir string, minFreeSpace uint64) *LocalStore {
	if minFreeSpace == 0 {
		minFreeSpace = defaultMinFreeSpace
	}
	logger.Debugw("Instantiated local store", "dir", dir)
	return &LocalStore{
		dir:          dir,
		minFreeSpace: minFreeSpace,
	}
}

// Dir returns the local directory path used by the store.
func (l *LocalStore) Dir() string {
	return l.dir
}

// Put reads the given reader fully and stores its content in the store directory as flat files.
//
// The reader content is first stored in a temporary directory and upon
// successful storage is moved to the store directory. The
// Descriptor.ModificationTime is set to the modification date of the file that
// corresponds to the content. The Descriptor.ID is randomly generated; see
// NewID.
//
// Before a blob is written, the minimum amount of free space must be available
// on the local disk. If writing the blob consumes more then the available
// space (free space - minimum free), then this results in an error.
func (l *LocalStore) Put(_ context.Context, reader io.ReadCloser) (*Descriptor, error) {
	usage, err := disk.Usage(l.dir)
	if err != nil {
		return nil, fmt.Errorf("cannot get disk usage: %w", err)
	}
	if usage.Free <= l.minFreeSpace {
		return nil, ErrNotEnoughSpace
	}

	dest, err := os.CreateTemp(l.dir, "motion_local_store_*.bin.temp")
	if err != nil {
		return nil, err
	}
	defer dest.Close()

	// Do not write more than the remaining storage - minimum free space.
	limit := int64(usage.Free - l.minFreeSpace)
	limitReader := io.LimitReader(reader, limit)

	written, err := io.Copy(dest, limitReader)
	if err != nil {
		os.Remove(dest.Name())
		return nil, err
	}
	if written == limit {
		os.Remove(dest.Name())
		return nil, ErrBlobTooLarge
	}

	id, err := NewID()
	if err != nil {
		return nil, err
	}
	if err = os.Rename(dest.Name(), filepath.Join(l.dir, id.String()+".bin")); err != nil {
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

// Get Retrieves the content of blob.
// If no blob is found for the given id, ErrBlobNotFound is returned.
func (l *LocalStore) Get(_ context.Context, id ID) (io.ReadSeekCloser, error) {
	blob, err := os.Open(filepath.Join(l.dir, id.String()+".bin"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	return blob, nil
}

// Describe gets the description of the blob for the given id.
// If no blob is found for the given id, ErrBlobNotFound is returned.
func (l *LocalStore) Describe(ctx context.Context, id ID) (*Descriptor, error) {
	stat, err := os.Stat(filepath.Join(l.dir, id.String()+".bin"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	return &Descriptor{
		ID:               id,
		Size:             uint64(stat.Size()),
		ModificationTime: stat.ModTime(),
	}, nil
}
