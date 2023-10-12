package test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/motion/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type BoostPublishQueryDeal struct {
	ID string
}

type BoostPublishQueryDealPublish struct {
	Deals []BoostPublishQueryDeal
}

type BoostPublishQueryData struct {
	DealPublish BoostPublishQueryDealPublish `json:"dealPublish"`
}
type BoostPublishQuery struct {
	Data BoostPublishQueryData `json:"data"`
}

func TestRoundTripPutStatusAndFullStorage(t *testing.T) {
	env := NewEnvironment(t)

	// make an 8MB random file -- to trigger at least one car generation
	wantBlob, err := io.ReadAll(io.LimitReader(rand.Reader, 8<<20))
	require.NoError(t, err)
	buf := bytes.NewBuffer(wantBlob)

	// find out how many deals we're starting with
	t.Log("check existing deals")
	var existingDeals int
	{
		req, err := http.NewRequest("GET", "http://localhost:8080/graphql/query", strings.NewReader("{\"operationName\":\"AppDealPublishQuery\",\"variables\":{},\"query\":\"query AppDealPublishQuery {  dealPublish {   Deals {      ID  __typename    }    __typename  }}\"}"))
		require.NoError(t, err)
		getResp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.EqualValues(t, http.StatusOK, getResp.StatusCode)
		decoder := json.NewDecoder(getResp.Body)
		var bpq BoostPublishQuery
		err = decoder.Decode(&bpq)
		require.NoError(t, err)
		existingDeals = len(bpq.Data.DealPublish.Deals)
	}

	// force boost to clear any publishable deals from singularity
	t.Log("clearing boost publish queue")
	{
		postResp, err := http.Post("http://localhost:8080/graphql/query", "application/json", strings.NewReader("{\"operationName\":\"AppDealPublishNowMutation\",\"variables\":{},\"query\":\"mutation AppDealPublishNowMutation {  dealPublishNow }\"}"))
		require.NoError(t, err)
		require.EqualValues(t, http.StatusOK, postResp.StatusCode)
		require.NoError(t, postResp.Body.Close())
	}

	var postBlobResp api.PostBlobResponse
	t.Log("posting 8MB data into motion")
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
	t.Log("requesting data back while still in source storage")
	{
		getResp, err := http.Get(requireJoinUrlPath(t, env.MotionAPIEndpoint, "v0", "blob", postBlobResp.ID))
		require.NoError(t, err)
		defer func() { require.NoError(t, getResp.Body.Close()) }()
		require.EqualValues(t, http.StatusOK, getResp.StatusCode)
		gotBlob, err = io.ReadAll(getResp.Body)
		require.NoError(t, err)
	}
	require.Equal(t, wantBlob, gotBlob)

	// get the status, and continue to check until we verify a deal was at least proposed through boost
	t.Log("waiting for singularity to make deals with boost")
	{
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			getResp, err := http.Get(requireJoinUrlPath(t, env.MotionAPIEndpoint, "v0", "blob", postBlobResp.ID, "status"))
			assert.NoError(c, err)
			defer func() { assert.NoError(c, getResp.Body.Close()) }()
			assert.EqualValues(c, http.StatusOK, getResp.StatusCode)
			jsonResp := json.NewDecoder(getResp.Body)
			var decoded api.GetStatusResponse
			err = jsonResp.Decode(&decoded)
			assert.NoError(c, err)
			assert.Equal(c, 2, len(decoded.Replicas))
			assert.Equal(c, 1, len(decoded.Replicas[0].Pieces))
			assert.Equal(c, 1, len(decoded.Replicas[1].Pieces))
		}, 2*time.Minute, 5*time.Second, "never initiated deal making")
	}

	// wait for deals to transfer to boost
	t.Log("waiting for singularity to transfer data to boost")
	{
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			req, err := http.NewRequest("GET", "http://localhost:8080/graphql/query", strings.NewReader("{\"operationName\":\"AppDealPublishQuery\",\"variables\":{},\"query\":\"query AppDealPublishQuery {  dealPublish {   Deals {      ID  __typename    }    __typename  }}\"}"))
			assert.NoError(c, err)
			getResp, err := http.DefaultClient.Do(req)
			assert.NoError(c, err)
			assert.EqualValues(c, http.StatusOK, getResp.StatusCode)
			decoder := json.NewDecoder(getResp.Body)
			var bpq BoostPublishQuery
			decoder.Decode(&bpq)
			assert.Len(c, bpq.Data.DealPublish.Deals, existingDeals+2)
		}, 2*time.Minute, 5*time.Second, "never finished data transfer")
	}

	// force boost to publish the deals from singularity
	t.Log("triggering data publish in boost")
	{

		postResp, err := http.Post("http://localhost:8080/graphql/query", "application/json", strings.NewReader("{\"operationName\":\"AppDealPublishNowMutation\",\"variables\":{},\"query\":\"mutation AppDealPublishNowMutation {  dealPublishNow}\"}"))
		require.NoError(t, err)
		require.EqualValues(t, http.StatusOK, postResp.StatusCode)
		require.NoError(t, postResp.Body.Close())
	}

	// await publishing
	t.Log("awaiting successful deal publishing")
	{
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			getResp, err := http.Get(requireJoinUrlPath(t, env.MotionAPIEndpoint, "v0", "blob", postBlobResp.ID, "status"))
			assert.NoError(c, err)
			defer func() { assert.NoError(c, getResp.Body.Close()) }()
			assert.EqualValues(c, http.StatusOK, getResp.StatusCode)
			jsonResp := json.NewDecoder(getResp.Body)
			var decoded api.GetStatusResponse
			err = jsonResp.Decode(&decoded)
			assert.NoError(c, err)
			assert.Equal(c, 2, len(decoded.Replicas), 2)
			for _, replica := range decoded.Replicas {
				assert.Equal(c, 1, len(replica.Pieces))
				assert.Contains(c, []string{"published", "active"}, replica.Pieces[0].Status)
			}
		}, 2*time.Minute, 5*time.Second, "published deals")
	}

	// this is just to allow the cleanup worker to run
	t.Log("sleeping for 1 minute to allow cleanup worker to run, and indexing to complete")
	time.Sleep(1 * time.Minute)

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
