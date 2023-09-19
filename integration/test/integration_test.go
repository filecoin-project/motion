package test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/filecoin-project/motion/api"
	"github.com/stretchr/testify/require"
)

// TODO: Expand test cases to assert singularity store start-up operations, e.g.:
//         * motion dataset exists
//         * schedules are created

func TestRoundTripPutAndGet(t *testing.T) {
	env := NewEnvironment(t)

	wantBlob, err := io.ReadAll(io.LimitReader(rand.Reader, 10<<20))
	require.NoError(t, err)
	buf := bytes.NewBuffer(wantBlob)

	var postBlobResp api.PostBlobResponse
	{
		postResp, err := http.Post(requireJoinUrlPath(t, env.MotionAPIEndpoint, "v0", "blob"), "application/octet-stream", buf)
		require.NoError(t, err)
		defer func() { require.NoError(t, postResp.Body.Close()) }()
		require.EqualValues(t, http.StatusCreated, postResp.StatusCode)
		require.NoError(t, json.NewDecoder(postResp.Body).Decode(&postBlobResp))
		t.Log(postBlobResp)
		require.NotEmpty(t, postBlobResp.ID)
	}

	var gotBlob []byte
	{
		getResp, err := http.Get(requireJoinUrlPath(t, env.MotionAPIEndpoint, "v0", "blob", postBlobResp.ID))
		require.NoError(t, err)
		defer func() { require.NoError(t, getResp.Body.Close()) }()
		require.EqualValues(t, http.StatusOK, getResp.StatusCode)
		gotBlob, err = io.ReadAll(getResp.Body)
		require.NoError(t, err)
	}
	require.Equal(t, wantBlob, gotBlob)
}

func requireJoinUrlPath(t *testing.T, base string, elem ...string) string {
	joined, err := url.JoinPath(base, elem...)
	require.NoError(t, err)
	return joined
}
