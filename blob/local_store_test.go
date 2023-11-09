package blob_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/filecoin-project/motion/blob"
	"github.com/gammazero/fsutil/disk"
	"github.com/stretchr/testify/require"
)

func TestWriteOK(t *testing.T) {
	tmpDir := t.TempDir()

	store := blob.NewLocalStore(tmpDir)
	buf := []byte("This is a test")
	readCloser := io.NopCloser(bytes.NewReader(buf))

	desc, err := store.Put(context.Background(), readCloser)
	require.NoError(t, err)
	require.NotNil(t, desc)
	require.Equal(t, uint64(len(buf)), desc.Size)
}

func TestInsufficientSpace(t *testing.T) {
	tmpDir := t.TempDir()
	usage, err := disk.Usage(tmpDir)
	require.NoError(t, err)

	store := blob.NewLocalStore(tmpDir, blob.WithMinFreeSpace(int64(usage.Free+blob.Gib)))
	readCloser := io.NopCloser(bytes.NewReader([]byte("This is a test")))

	_, err = store.Put(context.Background(), readCloser)
	require.ErrorIs(t, err, blob.ErrNotEnoughSpace)
}

func TestWriteTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	usage, err := disk.Usage(tmpDir)
	require.NoError(t, err)

	store := blob.NewLocalStore(tmpDir, blob.WithMinFreeSpace(int64(usage.Free-5*blob.Kib)))

	buf := make([]byte, 32*blob.Kib)
	readCloser := io.NopCloser(bytes.NewReader(buf))

	_, err = store.Put(context.Background(), readCloser)
	require.ErrorIs(t, err, blob.ErrBlobTooLarge)
}
