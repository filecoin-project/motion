package singularity_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	singularityclient "github.com/data-preservation-programs/singularity/client/swagger/http"
	"github.com/filecoin-project/motion/integration/singularity"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func checkGoLeaks(t *testing.T) {
	// Ignore goroutines that are already running.
	ignoreCurrent := goleak.IgnoreCurrent()
	// Check if new goroutines are still running at end of test.
	t.Cleanup(func() {
		goleak.VerifyNone(t, ignoreCurrent)
	})
}

func TestStorePut(t *testing.T) {
	checkGoLeaks(t)

	testServer := httptest.NewServer(http.HandlerFunc(testHandler))
	t.Cleanup(func() {
		testServer.Close()
	})

	cfg := singularityclient.DefaultTransportConfig()
	u, _ := url.Parse(testServer.URL)
	cfg.Host = u.Host
	singularityAPI := singularityclient.NewHTTPClientWithConfig(nil, cfg)

	tmpDir := t.TempDir()
	s, err := singularity.NewStore(
		singularity.WithStoreDir(tmpDir),
		singularity.WithWalletKey("dummy"),
		singularity.WithSingularityClient(singularityAPI),
	)
	require.NoError(t, err)

	ctx := context.Background()
	s.Start(ctx)

	testFile := filepath.Join(tmpDir, "testdata.txt")
	f, err := os.Create(testFile)
	require.NoError(t, err)
	_, err = f.WriteString("Halló heimur!")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	f, err = os.Open(testFile)
	require.NoError(t, err)
	t.Cleanup(func() {
		f.Close()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		// If this blocks, then store.toPack channel is not being read.
		for i := 0; i < 17; i++ {
			desc, err := s.Put(ctx, f)
			require.NoError(t, err)
			require.NotNil(t, desc)
		}
	}()

	timer := time.NewTimer(time.Second)
	select {
	case <-done:
	case <-timer.C:
		require.FailNow(t, "Put queue is not being read, check that store.runPreparationJobs is running")
	}
	timer.Stop()

	err = s.Shutdown(context.Background())
	require.NoError(t, err)
}

func testHandler(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	if req.URL.Path == "/api/preparation/MOTION_PREPARATION/schedules" && req.Method == http.MethodGet {
		http.Error(w, "", http.StatusNotFound)
		return
	}
}
