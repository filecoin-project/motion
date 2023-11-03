package singularity_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	singularityclient "github.com/data-preservation-programs/singularity/client/swagger/http"
	"github.com/filecoin-project/motion/blob"
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

	var blobID blob.ID
	done := make(chan struct{})
	go func() {
		defer close(done)
		// If this blocks, then store.toPack channel is not being read.
		for i := 0; i < 17; i++ {
			desc, err := s.Put(ctx, f)
			if i == 0 {
				blobID = desc.ID
			}
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

	// Check reading file ID.
	idStream, err := os.Open(filepath.Join(tmpDir, blobID.String()+".id"))
	require.NoError(t, err)
	fileIDString, err := io.ReadAll(idStream)
	require.NoError(t, err)
	fileID, err := strconv.ParseUint(string(fileIDString), 10, 64)
	require.NoError(t, err)
	require.Zero(t, fileID)

	err = s.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestReader(t *testing.T) {
	checkGoLeaks(t)

	testServer := httptest.NewServer(http.HandlerFunc(testHandler))
	t.Cleanup(func() {
		testServer.Close()
	})

	cfg := singularityclient.DefaultTransportConfig()
	u, _ := url.Parse(testServer.URL)
	cfg.Host = u.Host
	singularityAPI := singularityclient.NewHTTPClientWithConfig(nil, cfg)

	storeReader := singularity.NewReader(singularityAPI, 0, int64(len(testData)))
	defer storeReader.Close()

	outBuf := new(bytes.Buffer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		const readLen = 32

		for {
			// Read readLen bytes from storeReader.
			n, err := io.CopyN(outBuf, storeReader, readLen)
			if n < readLen || errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}
		_, err := io.CopyN(outBuf, storeReader, readLen)
		require.ErrorIs(t, err, io.EOF)
	}()

	timer := time.NewTimer(time.Second)
	select {
	case <-done:
	case <-timer.C:
		require.FailNow(t, "Put queue is not being read, check that store.runPreparationJobs is running")
	}
	timer.Stop()

	require.NoError(t, storeReader.Close())

	require.Equal(t, len(testData), outBuf.Len())
	require.Equal(t, outBuf.Bytes(), testData)
}

var testData = []byte(`
RGVzaXJlLCB0byBrbm93IHdoeSwgYW5kIGhvdywgY3VyaW9zaXR5OyBzdWNoIGFzIGlzIGluIG5vIGx
pdmluZyBjcmVhdHVyZSBidXQgbWFuOiBzbyB0aGF0IG1hbiBpcyBkaXN0aW5ndWlzaGVkLCBub3Qgb2
5seSBieSBoaXMgcmVhc29uOyBidXQgYWxzbyBieSB0aGlzIHNpbmd1bGFyIHBhc3Npb24gZnJvbSBvd
GhlciBhbmltYWxzOyBpbiB3aG9tIHRoZSBhcHBldGl0ZSBvZiBmb29kLCBhbmQgb3RoZXIgcGxlYXN1
cmVzIG9mIHNlbnNlLCBieSBwcmVkb21pbmFuY2UsIHRha2UgYXdheSB0aGUgY2FyZSBvZiBrbm93aW5
nIGNhdXNlczsgd2hpY2ggaXMgYSBsdXN0IG9mIHRoZSBtaW5kLCB0aGF0IGJ5IGEgcGVyc2V2ZXJhbm
NlIG9mIGRlbGlnaHQgaW4gdGhlIGNvbnRpbnVhbCBhbmQgaW5kZWZhdGlnYWJsZSBnZW5lcmF0aW9uI
G9mIGtub3dsZWRnZSwgZXhjZWVkZXRoIHRoZSBzaG9ydCB2ZWhlbWVuY2Ugb2YgYW55IGNhcm5hbCBw
bGVhc3VyZS4K`)

func testHandler(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	switch req.URL.Path {
	case "/api/identity":
		if req.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	case "/api/preparation/MOTION_PREPARATION/schedules":
		if req.Method == http.MethodGet {
			http.Error(w, "", http.StatusNotFound)
			return
		}
	case "/api/file/0/retrieve":
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(testData)
		return
	}
}
