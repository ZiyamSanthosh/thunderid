// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package apiaccess

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/tests/integration/testutils"
)

const apiAccessConfigKey = "api_access"

type apiAccessError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

// APIAccessTestSuite verifies that login-only mode is loaded by a real ThunderID process and
// restricts the exposed HTTP surface without preventing login and token issuance.
type APIAccessTestSuite struct {
	suite.Suite
	client         *http.Client
	originalConfig interface{}
}

func TestAPIAccessTestSuite(t *testing.T) {
	suite.Run(t, new(APIAccessTestSuite))
}

func (ts *APIAccessTestSuite) SetupSuite() {
	ts.client = testutils.GetRawHTTPClient()

	original, err := testutils.ReadDeploymentConfigKey(apiAccessConfigKey)
	ts.Require().NoError(err, "failed to read the API access configuration")
	ts.originalConfig = original

	ts.Require().NoError(testutils.PatchDeploymentConfig(map[string]interface{}{
		apiAccessConfigKey: map[string]interface{}{"mode": "login_only"},
	}), "failed to enable login-only API access")
	ts.Require().NoError(testutils.RestartServer(), "failed to restart ThunderID in login-only mode")

	// This performs the normal password-based authentication flow and token exchange. Its success
	// proves that login and token issuance remain functional after the API restriction is active.
	ts.Require().NoError(testutils.ObtainAdminAccessToken(),
		"failed to obtain an access token in login-only mode")
}

func (ts *APIAccessTestSuite) TearDownSuite() {
	// Use non-fatal assertions so every restoration step is attempted even if an earlier one fails.
	ts.Assert().NoError(testutils.PatchDeploymentConfig(map[string]interface{}{
		apiAccessConfigKey: ts.originalConfig,
	}), "teardown: failed to restore the API access configuration")
	ts.Assert().NoError(testutils.RestartServer(),
		"teardown: failed to restart ThunderID after restoring API access")
	ts.Assert().NoError(testutils.ObtainAdminAccessToken(),
		"teardown: failed to restore the shared admin access token")
}

func (ts *APIAccessTestSuite) TestLoginSupportEndpointsRemainAvailable() {
	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "liveness", method: http.MethodGet, path: "/health/liveness"},
		{name: "OIDC discovery", method: http.MethodGet, path: "/.well-known/openid-configuration"},
		{name: "flow execution", method: http.MethodPost, path: "/flow/execute", body: []byte(`{}`)},
		{name: "token endpoint", method: http.MethodPost, path: "/oauth2/token"},
	}

	for _, tt := range tests {
		ts.Run(tt.name, func() {
			resp, body := ts.send(tt.method, tt.path, tt.body)
			defer resp.Body.Close()

			ts.NotEqual(http.StatusServiceUnavailable, resp.StatusCode,
				"%s must reach its registered handler; body: %s", tt.path, body)
			ts.NotContains(string(body), `"error":"api_restricted"`)
		})
	}
}

func (ts *APIAccessTestSuite) TestManagementEndpointsAreRestricted() {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "applications", method: http.MethodGet, path: "/applications"},
		{name: "users", method: http.MethodGet, path: "/users"},
		{name: "server configuration", method: http.MethodGet, path: "/server-config"},
		{name: "dynamic client registration", method: http.MethodPost, path: "/oauth2/dcr/register"},
		{name: "MCP", method: http.MethodPost, path: "/mcp"},
		{name: "Console", method: http.MethodGet, path: "/console/"},
	}

	for _, tt := range tests {
		ts.Run(tt.name, func() {
			resp, body := ts.send(tt.method, tt.path, nil)
			defer resp.Body.Close()

			ts.Equal(http.StatusServiceUnavailable, resp.StatusCode,
				"%s should be restricted; body: %s", tt.path, body)

			var errResp apiAccessError
			ts.Require().NoError(json.Unmarshal(body, &errResp))
			ts.Equal("api_restricted", errResp.Code)
			ts.NotEmpty(errResp.Description)
		})
	}
}

func (ts *APIAccessTestSuite) send(method, path string, body []byte) (*http.Response, []byte) {
	req, err := http.NewRequest(method, testutils.TestServerURL+path, bytes.NewReader(body))
	ts.Require().NoError(err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := ts.client.Do(req)
	ts.Require().NoError(err)

	responseBody, err := io.ReadAll(resp.Body)
	ts.Require().NoError(err)
	resp.Body = io.NopCloser(bytes.NewReader(responseBody))
	return resp, responseBody
}
