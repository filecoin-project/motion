package test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type testCase struct {
	name          string
	onMethod      string
	onPath        string
	onBody        string
	onContentType string
	expectStatus  int
	expectBody    string // may be regex. optional (empty = not tested)

	// Silences error for this schema case
	skip bool
}

type schemaCase struct {
	method  string
	path    string
	status  string
	covered bool
}

func (s schemaCase) String() string {
	return fmt.Sprintf("%s %s -> %s (covered: %v)\n", s.method, s.path, s.status, s.covered)
}

func TestApi(t *testing.T) {
	env := NewEnvironment(t)

	// ---- Add test cases here ----
	tests := []testCase{
		{
			name:          "POST /v0/blob is 201",
			onMethod:      http.MethodPost,
			onPath:        "/v0/blob",
			onBody:        "fish",
			onContentType: "application/octet-stream",
			expectBody:    "{\"id\":\".*\"}",
			expectStatus:  201,
		},
		{
			// not reliably testable
			onMethod:     http.MethodPost,
			onPath:       "/v0/blob",
			expectStatus: 500,
			skip:         true,
		},
		{
			// not reliably testable
			onMethod:     http.MethodPost,
			onPath:       "/v0/blob",
			expectStatus: 503,
			skip:         true,
		},
		{
			// tested in integration
			name:         "GET /v0/blob/{id} is 200",
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000",
			expectStatus: 200,
			skip:         true,
		},
		{
			name:         "GET /v0/blob/{id} for unknown ID is 404",
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000",
			expectStatus: 404,
		},
		{
			// not reliably testable
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000",
			expectStatus: 500,
			skip:         true,
		},
		{
			// not reliably testable
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000",
			expectStatus: 503,
			skip:         true,
		},
		{
			// tested in integration
			name:         "GET /v0/blob/{id}/status is 200",
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000/status",
			expectStatus: 200,
			skip:         true,
		},
		{
			// tested in integration
			name:         "GET /v0/blob/{id}/status for unknown ID is 404",
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000/status",
			expectStatus: 404,
		},
		{
			// not reliably testable
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000/status",
			expectStatus: 500,
			skip:         true,
		},
		{
			// not reliably testable
			onMethod:     http.MethodGet,
			onPath:       "/v0/blob/00000000-0000-0000-0000-000000000000/status",
			expectStatus: 503,
			skip:         true,
		},
	}

	// Read and parse openapi.yaml for ensuring all paths, methods, and status
	// codes are covered

	schemaString, err := os.ReadFile("../../openapi.yaml")
	require.NoError(t, err, "could not find openapi.yaml")

	schemaMap := make(map[string]interface{})
	err = yaml.Unmarshal(schemaString, schemaMap)
	require.NoError(t, err)

	var schemaCases []schemaCase

	type kvmap = map[string]interface{}
	for pathName, path := range schemaMap["paths"].(kvmap) {
		for methodName, method := range path.(kvmap) {
			for statusCode := range method.(kvmap)["responses"].(kvmap) {
				schemaCases = append(schemaCases, schemaCase{
					method:  methodName,
					path:    pathName,
					status:  statusCode,
					covered: false,
				})
			}
		}
	}

	// Run all tests
	for _, test := range tests {
		if !test.skip {
			req, err := http.NewRequest(
				test.onMethod,
				requireJoinUrlPath(t, env.MotionAPIEndpoint, test.onPath),
				bytes.NewReader([]byte(test.expectBody)),
			)
			req.Header.Set("Content-Type", test.onContentType)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)

			// Body must be as expected
			var body string
			if test.expectBody != "" {
				bodyBytes, err := io.ReadAll(resp.Body)
				require.NoError(t, err)

				body = string(bodyBytes)
				require.Regexp(t, test.expectBody, string(body))
			}

			// Status code must be as expected
			require.Equal(t, test.expectStatus, resp.StatusCode, "Incorrect status code for test %#v (resp body: %v)", test, body)
		}

		// Find matching schema case and mark as covered
		for i := range schemaCases {
			methodsMatch := strings.EqualFold(schemaCases[i].method, test.onMethod)
			pathsMatch := schemaPathFitsTest(schemaCases[i].path, test.onPath)
			statusesMatch := schemaCases[i].status == strconv.Itoa(test.expectStatus)

			if methodsMatch && pathsMatch && statusesMatch {
				schemaCases[i].covered = true
				break
			}
		}
	}

	// Make sure all schema cases are covered
	var notCovered []schemaCase
	for _, schemaCase := range schemaCases {
		if !schemaCase.covered {
			notCovered = append(notCovered, schemaCase)
		}
	}

	require.Empty(t, notCovered, "all schema cases must be covered")
}

// Checks whether a test's path fits into a schema path listed in openapi.yaml,
// where the schema path may have variable parts (example, schema /foo/bar/{x}
// == test /foo/bar/5).
func schemaPathFitsTest(schemaPath string, testPath string) bool {
	schemaParts := strings.Split(schemaPath, "/")
	testParts := strings.Split(testPath, "/")

	if len(schemaParts) != len(testParts) {
		return false
	}

	for i := range schemaParts {
		if schemaParts[i] == testParts[i] {
			continue
		} else if schemaParts[i][0] == '{' {
			continue
		} else {
			return false
		}
	}

	return true
}
