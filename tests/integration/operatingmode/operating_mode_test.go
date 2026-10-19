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

	flowcommon "github.com/thunder-id/thunderid/tests/integration/flow/common"
	"github.com/thunder-id/thunderid/tests/integration/testutils"
)

const (
	operatingModeConfigKey       = "operating_mode"
	restrictedTestUsername       = "restricted-mode-user"
	restrictedTestPassword       = "RestrictedModePassword123!"
	restrictedTestAttributeValue = "must-not-be-persisted"
)

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
	client                  *http.Client
	originalConfig          interface{}
	ouID                    string
	userTypeID              string
	userID                  string
	attributeCollectorAppID string
	directFenceApps         map[string]string
	applicationIDs          []string
	flowIDs                 []string
}

func TestOperatingModeTestSuite(t *testing.T) {
	suite.Run(t, new(OperatingModeTestSuite))
}

func (ts *OperatingModeTestSuite) SetupSuite() {
	ts.client = testutils.GetRawHTTPClient()
	ts.directFenceApps = make(map[string]string)

	original, err := testutils.ReadDeploymentConfigKey(operatingModeConfigKey)
	ts.Require().NoError(err, "failed to read the operating mode")
	ts.originalConfig = original
	ts.createCapabilityFenceFixtures()

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
	ts.assertAttributeWasNotPersisted()
	ts.cleanupCapabilityFenceFixtures()
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

func (ts *OperatingModeTestSuite) TestAttributeCollectionIsFencedBeforeMutation() {
	step, err := flowcommon.InitiateAuthenticationFlow(ts.attributeCollectorAppID, false, nil, "")
	ts.Require().NoError(err)
	ts.Require().Equal("INCOMPLETE", step.FlowStatus)

	step, err = flowcommon.CompleteFlow(step.ExecutionID, map[string]string{
		"username": restrictedTestUsername,
		"password": restrictedTestPassword,
	}, "", step.ChallengeToken)
	ts.Require().NoError(err)
	ts.Require().Equal("INCOMPLETE", step.FlowStatus)
	ts.Require().True(flowcommon.HasInput(step.Data.Inputs, "nickname"))

	step, err = flowcommon.CompleteFlow(step.ExecutionID, map[string]string{
		"nickname": restrictedTestAttributeValue,
	}, "", step.ChallengeToken)
	ts.Require().NoError(err)
	ts.requireFlowError(step, "FET-1115")
}

func (ts *OperatingModeTestSuite) TestExecutorCapabilitiesAreFencedInsideAuthenticationFlows() {
	tests := []struct {
		name string
		app  string
		code string
	}{
		{name: "organization unit creation", app: "ou", code: "FET-1113"},
		{name: "credential update", app: "credential", code: "FET-1116"},
		{name: "passkey authentication", app: "passkey", code: "FET-1117"},
	}

	for _, tt := range tests {
		ts.Run(tt.name, func() {
			step, err := flowcommon.InitiateAuthenticationFlow(ts.directFenceApps[tt.app], false, nil, "")
			ts.Require().NoError(err)
			ts.requireFlowError(step, tt.code)
		})
	}
}

func (ts *OperatingModeTestSuite) requireFlowError(step *flowcommon.FlowStep, code string) {
	ts.Require().Equal("ERROR", step.FlowStatus)
	ts.Require().NotNil(step.Error)
	ts.Equal(code, step.Error.Code)
}

func (ts *OperatingModeTestSuite) createCapabilityFenceFixtures() {
	ouID, err := testutils.CreateOrganizationUnit(testutils.OrganizationUnit{
		Handle: "restricted-mode-capability-test",
		Name:   "Restricted Mode Capability Test",
	})
	ts.Require().NoError(err)
	ts.ouID = ouID

	userType := testutils.UserType{
		Handle:      "restricted_mode_user",
		DisplayName: "Restricted Mode User",
		OUID:        ouID,
		Schema: map[string]interface{}{
			"username": map[string]interface{}{"type": "string"},
			"password": map[string]interface{}{"type": "string", "credential": true},
			"nickname": map[string]interface{}{"type": "string"},
		},
	}
	userTypeID, err := testutils.CreateUserType(userType)
	ts.Require().NoError(err)
	ts.userTypeID = userTypeID

	userID, err := testutils.CreateUser(testutils.User{
		OUID: ouID,
		Type: userType.Handle,
		Attributes: json.RawMessage(`{
			"username":"` + restrictedTestUsername + `",
			"password":"` + restrictedTestPassword + `"
		}`),
	})
	ts.Require().NoError(err)
	ts.userID = userID

	ts.attributeCollectorAppID = ts.createFlowApplication("restricted-attribute", userType.Handle,
		attributeCollectorFlow())
	ts.directFenceApps["ou"] = ts.createFlowApplication("restricted-ou", userType.Handle,
		directFenceFlow("restricted_ou_creation", "OUExecutor", "", nil))
	ts.directFenceApps["credential"] = ts.createFlowApplication("restricted-credential", userType.Handle,
		directFenceFlow("restricted_credential_update", "CredentialSetter", "", nil))
	ts.directFenceApps["passkey"] = ts.createFlowApplication("restricted-passkey", userType.Handle,
		directFenceFlow("restricted_passkey_authentication", "PasskeyAuthExecutor", "challenge", map[string]interface{}{
			"relyingPartyId": "localhost",
		}))
}

func (ts *OperatingModeTestSuite) createFlowApplication(handle, userType string, flow testutils.Flow) string {
	flowID, err := testutils.CreateFlow(flow)
	ts.Require().NoError(err)
	ts.flowIDs = append(ts.flowIDs, flowID)

	appID, err := testutils.CreateApplication(testutils.Application{
		Name:             "Restricted Mode " + handle,
		Description:      "Exercises a restricted-mode capability fence",
		OUID:             ts.ouID,
		AuthFlowID:       flowID,
		ClientID:         handle,
		ClientSecret:     "restricted-mode-test-secret",
		RedirectURIs:     []string{"http://localhost:3000/callback"},
		AllowedUserTypes: []string{userType},
	})
	ts.Require().NoError(err)
	ts.applicationIDs = append(ts.applicationIDs, appID)
	return appID
}

func attributeCollectorFlow() testutils.Flow {
	return testutils.Flow{
		Name:     "Restricted Attribute Collector",
		Handle:   "restricted_attribute_collector",
		FlowType: "AUTHENTICATION",
		Nodes: []map[string]interface{}{
			{"id": "start", "type": "START", "onSuccess": "authenticate"},
			{
				"id":   "authenticate",
				"type": "TASK_EXECUTION",
				"executor": map[string]interface{}{
					"name": "CredentialsAuthExecutor",
					"inputs": []map[string]interface{}{
						{"ref": "username", "identifier": "username", "type": "TEXT_INPUT", "required": true},
						{"ref": "password", "identifier": "password", "type": "PASSWORD_INPUT", "required": true},
					},
				},
				"onSuccess": "collect",
			},
			{
				"id":   "collect",
				"type": "TASK_EXECUTION",
				"executor": map[string]interface{}{
					"name": "AttributeCollector",
					"inputs": []map[string]interface{}{
						{"ref": "nickname", "identifier": "nickname", "type": "TEXT_INPUT", "required": true},
					},
				},
				"onSuccess": "assert",
			},
			{"id": "assert", "type": "TASK_EXECUTION",
				"executor": map[string]interface{}{"name": "AuthAssertExecutor"}, "onSuccess": "end"},
			{"id": "end", "type": "END"},
		},
	}
}

func directFenceFlow(handle, executor, mode string, properties map[string]interface{}) testutils.Flow {
	executorConfig := map[string]interface{}{"name": executor}
	if mode != "" {
		executorConfig["mode"] = mode
	}
	node := map[string]interface{}{
		"id":        "fenced",
		"type":      "TASK_EXECUTION",
		"executor":  executorConfig,
		"onSuccess": "assert",
	}
	if properties != nil {
		node["properties"] = properties
	}

	return testutils.Flow{
		Name:     "Restricted " + executor,
		Handle:   handle,
		FlowType: "AUTHENTICATION",
		Nodes: []map[string]interface{}{
			{"id": "start", "type": "START", "onSuccess": "fenced"},
			node,
			{"id": "assert", "type": "TASK_EXECUTION",
				"executor": map[string]interface{}{"name": "AuthAssertExecutor"}, "onSuccess": "end"},
			{"id": "end", "type": "END"},
		},
	}
}

func (ts *OperatingModeTestSuite) assertAttributeWasNotPersisted() {
	if ts.userID == "" {
		return
	}
	user, err := testutils.FindUserByAttribute("username", restrictedTestUsername)
	ts.Assert().NoError(err, "teardown: failed to read the capability-test user")
	if user == nil {
		ts.Fail("teardown: capability-test user was not found")
		return
	}
	attrs, err := testutils.GetUserAttributes(*user)
	ts.Assert().NoError(err, "teardown: failed to parse the capability-test user")
	ts.NotContains(attrs, "nickname", "restricted attribute collection must not mutate the entity")
}

func (ts *OperatingModeTestSuite) cleanupCapabilityFenceFixtures() {
	for _, appID := range ts.applicationIDs {
		ts.Assert().NoError(testutils.DeleteApplication(appID), "teardown: failed to delete application %s", appID)
	}
	for _, flowID := range ts.flowIDs {
		ts.Assert().NoError(testutils.DeleteFlow(flowID), "teardown: failed to delete flow %s", flowID)
	}
	if ts.userID != "" {
		ts.Assert().NoError(testutils.DeleteUser(ts.userID), "teardown: failed to delete the capability-test user")
	}
	if ts.userTypeID != "" {
		ts.Assert().NoError(testutils.DeleteUserType(ts.userTypeID),
			"teardown: failed to delete the capability-test user type")
	}
	if ts.ouID != "" {
		ts.Assert().NoError(testutils.DeleteOrganizationUnit(ts.ouID),
			"teardown: failed to delete the capability-test organization unit")
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
