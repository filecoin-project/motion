package singularity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/filecoin-project/motion/blob"
)

type cleanupSchedulerConfig struct {
	interval time.Duration
}

// This is run by the cleanup scheduler to determine whether to clean up a local
// file.
type cleanupReadyCallback func(ctx context.Context, blobID blob.ID) (bool, error)

type cleanupScheduler struct {
	cfg          cleanupSchedulerConfig
	local        *blob.LocalStore
	cleanupReady cleanupReadyCallback
	closing      chan struct{}
	closed       sync.WaitGroup
}

func newCleanupScheduler(
	cfg cleanupSchedulerConfig,
	local *blob.LocalStore,
	cleanupReady cleanupReadyCallback,
) *cleanupScheduler {
	return &cleanupScheduler{
		cfg:          cfg,
		local:        local,
		cleanupReady: cleanupReady,
		closing:      make(chan struct{}),
	}
}

func (cs *cleanupScheduler) start(ctx context.Context) {
	cs.closed.Add(1)

	go func() {
		defer cs.closed.Done()

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-cs.closing
			cancel()
		}()

		ticker := time.NewTicker(cs.cfg.interval)

		// Run once immediately on startup
		cs.cleanup(ctx)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cs.cleanup(ctx)
			}
		}
	}()
}

func (cs *cleanupScheduler) stop(ctx context.Context) error {
	close(cs.closing)

	done := make(chan struct{})
	go func() {
		cs.closed.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (cs *cleanupScheduler) cleanup(ctx context.Context) error {
	ids, err := cs.local.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list local blob IDs: %w", err)
	}

	var removals []blob.ID
	for _, id := range ids {
		cleanupReady, err := cs.cleanupReady(ctx, id)
		if err != nil {
			logger.Warnf("failed to check if blob is ready for cleanup, skipping for this cleanup cycle: %w", err)
			continue
		}
		if cleanupReady {
			removals = append(removals, id)
		}
	}

	for _, blobID := range removals {
		cs.local.Remove(ctx, blobID)
	}

	return nil
}
