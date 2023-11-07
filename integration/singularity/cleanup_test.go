package singularity

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/filecoin-project/motion/blob"
	"github.com/stretchr/testify/require"
)

func TestCleanupScheduler(t *testing.T) {
	localDir := filepath.Join(t.TempDir(), "motion-cleanup-test")
	require.NoError(t, os.MkdirAll(localDir, 0777))
	local := blob.NewLocalStore(localDir)

	var cleanupNotReadyBlobs []blob.ID
	var cleanupReadyBlobs []blob.ID

	// Pushes a new blob and optionally add it to the shouldRemove array for the
	// cleanup test
	add := func(shouldRemove bool) {
		data := make([]byte, 16)
		_, err := rand.Reader.Read(data)
		require.NoError(t, err)

		desc, err := local.Put(context.Background(), io.NopCloser(bytes.NewReader(data)))
		require.NoError(t, err)

		if shouldRemove {
			cleanupReadyBlobs = append(cleanupReadyBlobs, desc.ID)
		} else {
			cleanupNotReadyBlobs = append(cleanupNotReadyBlobs, desc.ID)
		}
	}

	// Intersperse ready and non-ready blobs
	for i := 0; i < 100; i++ {
		add(false)
		add(true)
	}

	// Returns true if the value is present in the shouldRemove slice
	callback := func(ctx context.Context, blobID blob.ID) (bool, error) {
		for _, other := range cleanupReadyBlobs {
			if blobID == other {
				return true, nil
			}
		}
		return false, nil
	}

	cfg := cleanupSchedulerConfig{
		interval: time.Second,
	}

	// Before starting the cleanup scheduler, make sure all blobs are present
	listBefore, err := local.List(context.Background())
	t.Logf("length before: %v", len(listBefore))
	require.NoError(t, err)
	for _, blob := range cleanupNotReadyBlobs {
		require.Contains(t, listBefore, blob)
	}
	for _, blob := range cleanupReadyBlobs {
		require.Contains(t, listBefore, blob)
	}

	// Start cleanup scheduler and check that all cleanup ready blobs are
	// already removed before the first cleanup tick, since there should be one
	// iteration immediately on startup
	cleanupScheduler := newCleanupScheduler(cfg, local, callback)
	cleanupScheduler.start(context.Background())

	time.Sleep(cfg.interval / 2)

	listAfterStart, err := local.List(context.Background())
	t.Logf("length after start: %v", len(listAfterStart))
	require.NoError(t, err)
	for _, blob := range cleanupNotReadyBlobs {
		require.Contains(t, listAfterStart, blob)
	}
	for _, blob := range cleanupReadyBlobs {
		require.NotContains(t, listAfterStart, blob)
	}

	// Add the rest of the blobs to cleanup ready, wait for 1 more tick, and
	// make sure that no more blobs are left
	cleanupReadyBlobs = append(cleanupReadyBlobs, cleanupNotReadyBlobs...)

	time.Sleep(cfg.interval)

	listAfterTick, err := local.List(context.Background())
	t.Logf("length after tick: %v", len(listAfterTick))
	require.NoError(t, err)
	require.Empty(t, listAfterTick)
}
