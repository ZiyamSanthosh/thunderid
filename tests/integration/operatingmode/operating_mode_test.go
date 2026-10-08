// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operatingmode

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/tests/integration/testutils"
)

const operatingModeConfigKey = "operating_mode"

type operatingModeError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

type flowFeatureError struct {
	Code string `json:"code"`
}

// OperatingModeTestSuite verifies that restricted mode is loaded by a real ThunderID process and restricts
// the exposed HTTP surface without preventing authentication, authorization, and token operations.
type OperatingModeTestSuite struct {
	suite.Suite
	client         *http.Client
	originalConfig interface{}
}

func TestOperatingModeTestSuite(t *testing.T) {
	suite.Run(t, new(OperatingModeTestSuite))
}

func (ts *OperatingModeTestSuite) SetupSuite() {
	ts.client = testutils.GetRawHTTPClient()

	original, err := testutils.ReadDeploymentConfigKey(operatingModeConfigKey)
	ts.Require().NoError(err, "failed to read the operating mode")
	ts.originalConfig = original

	ts.Require().NoError(testutils.PatchDeploymentConfig(map[string]interface{}{
		operatingModeConfigKey: "restricted",
	}), "failed to enable restricted mode")
	ts.Require().NoError(testutils.RestartServer(), "failed to restart ThunderID in restricted mode")

	// This performs the normal password-based authentication flow and token exchange. Its success
	// proves that login and token issuance remain functional after the API restriction is active.
	ts.Require().NoError(testutils.ObtainAdminAccessToken(),
		"failed to obtain an access token in restricted mode")
}

func (ts *OperatingModeTestSuite) TearDownSuite() {
	// Use non-fatal assertions so every restoration step is attempted even if an earlier one fails.
	ts.Assert().NoError(testutils.PatchDeploymentConfig(map[string]interface{}{
		operatingModeConfigKey: ts.originalConfig,
	}), "teardown: failed to restore the operating mode")
	ts.Assert().NoError(testutils.RestartServer(),
		"teardown: failed to restart ThunderID after restoring the operating mode")
	ts.Assert().NoError(testutils.ObtainAdminAccessToken(),
		"teardown: failed to restore the shared admin access token")
}

func (ts *OperatingModeTestSuite) TestAuthenticationAuthorizationAndTokenEndpointsRemainAvailable() {
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
			ts.NotContains(string(body), `"error":"feature_unavailable"`)
		})
	}
}

func (ts *OperatingModeTestSuite) TestManagementEndpointsAreRestricted() {
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

			var errResp operatingModeError
			ts.Require().NoError(json.Unmarshal(body, &errResp))
			ts.Equal("feature_unavailable", errResp.Code)
			ts.NotEmpty(errResp.Description)
		})
	}
}

func (ts *OperatingModeTestSuite) TestNonAuthenticationFlowsAreRestricted() {
	for _, flowType := range []string{"REGISTRATION", "RECOVERY"} {
		ts.Run(flowType, func() {
			requestBody, err := json.Marshal(map[string]string{
				"applicationId": "01900000-0000-7000-8000-000000000060",
				"flowType":      flowType,
			})
			ts.Require().NoError(err)

			resp, body := ts.send(http.MethodPost, "/flow/execute", requestBody)
			defer resp.Body.Close()

			ts.Equal(http.StatusServiceUnavailable, resp.StatusCode, "body: %s", body)
			var errResp flowFeatureError
			ts.Require().NoError(json.Unmarshal(body, &errResp))
			ts.Equal("FES-1020", errResp.Code)
		})
	}
}

func (ts *OperatingModeTestSuite) send(method, path string, body []byte) (*http.Response, []byte) {
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
