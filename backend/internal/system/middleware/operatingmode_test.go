// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestrictedModeMiddlewareDisabled(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RestrictedModeMiddleware(false)(next)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/applications/example", nil))

	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestRestrictedModeMiddlewareEnabled(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "flow execution", path: "/flow/execute", wantStatus: http.StatusNoContent},
		{name: "token endpoint", path: "/oauth2/token", wantStatus: http.StatusNoContent},
		{name: "authorization endpoint", path: "/oauth2/authorize", wantStatus: http.StatusNoContent},
		{name: "discovery endpoint", path: "/.well-known/openid-configuration", wantStatus: http.StatusNoContent},
		{name: "gate root", path: "/gate", wantStatus: http.StatusNoContent},
		{name: "gate asset", path: "/gate/assets/main.js", wantStatus: http.StatusNoContent},
		{name: "health endpoint", path: "/health/readiness", wantStatus: http.StatusNoContent},
		{name: "design resolve", path: "/design/resolve", wantStatus: http.StatusNoContent},
		{name: "translation resolve", path: "/i18n/languages/en-US/translations/resolve", wantStatus: http.StatusNoContent},
		{name: "OU qualified token endpoint", method: http.MethodPost, path: "/ou/child-ou/oauth2/token",
			wantStatus: http.StatusNoContent},
		{name: "OU qualified nested path", method: http.MethodPost, path: "/ou/a/b/oauth2/token",
			wantStatus: http.StatusServiceUnavailable},
		{name: "OpenID4VP request", path: "/openid4vp/request", wantStatus: http.StatusNoContent},
		{name: "OpenID4VP response", method: http.MethodPost, path: "/openid4vp/response",
			wantStatus: http.StatusNoContent},
		{name: "OpenID4VP initiate", method: http.MethodPost, path: "/openid4vp/initiate",
			wantStatus: http.StatusNoContent},
		{name: "OpenID4VP status", path: "/openid4vp/status/txn-1", wantStatus: http.StatusNoContent},
		{name: "OpenID4VP trust anchors", path: "/openid4vp/trust-anchors", wantStatus: http.StatusServiceUnavailable},
		{name: "presentation definition creation", method: http.MethodPost, path: "/openid4vp/presentation-definitions",
			wantStatus: http.StatusServiceUnavailable},
		{name: "presentation definition update", method: http.MethodPut,
			path: "/openid4vp/presentation-definitions/example", wantStatus: http.StatusServiceUnavailable},
		{name: "presentation definition deletion", method: http.MethodDelete,
			path: "/openid4vp/presentation-definitions/example", wantStatus: http.StatusServiceUnavailable},
		{name: "application update", method: http.MethodPut, path: "/applications/example",
			wantStatus: http.StatusServiceUnavailable},
		{name: "user creation", method: http.MethodPost, path: "/users", wantStatus: http.StatusServiceUnavailable},
		{name: "user deletion", method: http.MethodDelete, path: "/users/example",
			wantStatus: http.StatusServiceUnavailable},
		{name: "application read", path: "/applications/example", wantStatus: http.StatusNoContent},
		{name: "application list", path: "/applications", wantStatus: http.StatusNoContent},
		{name: "connection read", path: "/connections/meta", wantStatus: http.StatusNoContent},
		{name: "flow version read", path: "/flows/example/versions/1", wantStatus: http.StatusNoContent},
		{name: "organization unit tree read", path: "/organization-units/tree/root", wantStatus: http.StatusNoContent},
		{name: "user type read", path: "/user-types/example", wantStatus: http.StatusNoContent},
		{name: "user read", path: "/users/example", wantStatus: http.StatusNoContent},
		{name: "user read with HEAD", method: http.MethodHead, path: "/users/example",
			wantStatus: http.StatusNoContent},
		{name: "user read preflight", method: http.MethodOptions, path: "/users", wantStatus: http.StatusNoContent},
		{name: "group read", path: "/groups/example", wantStatus: http.StatusNoContent},
		{name: "role read", path: "/roles", wantStatus: http.StatusNoContent},
		{name: "resource server action read", path: "/resource-servers/rs/actions/a", wantStatus: http.StatusNoContent},
		{name: "agent read", path: "/agents/example/roles", wantStatus: http.StatusNoContent},
		{name: "agent type read", path: "/agent-types", wantStatus: http.StatusNoContent},
		{name: "gateway read", path: "/gateways/example/diff", wantStatus: http.StatusNoContent},
		{name: "group update", method: http.MethodPut, path: "/groups/example",
			wantStatus: http.StatusServiceUnavailable},
		{name: "secret read", path: "/secrets/example", wantStatus: http.StatusServiceUnavailable},
		{name: "notification template read", path: "/notification-templates",
			wantStatus: http.StatusServiceUnavailable},
		{name: "server configuration read", path: "/server-config", wantStatus: http.StatusServiceUnavailable},
		{name: "export", method: http.MethodPost, path: "/export", wantStatus: http.StatusServiceUnavailable},
		{name: "prefix without a segment boundary", path: "/usersettings", wantStatus: http.StatusServiceUnavailable},
		{name: "organization unit lookalike", path: "/ou/example", wantStatus: http.StatusServiceUnavailable},
		{name: "dynamic client registration", path: "/oauth2/dcr/register", wantStatus: http.StatusServiceUnavailable},
		{name: "passkey registration", path: "/register/passkey/start", wantStatus: http.StatusServiceUnavailable},
		{name: "Console root", path: "/console", wantStatus: http.StatusNoContent},
		{name: "Console page", path: "/console/index.html", wantStatus: http.StatusNoContent},
		{name: "Console runtime config", path: "/console/config.js", wantStatus: http.StatusNoContent},
		{name: "Console asset", path: "/console/assets/index.js", wantStatus: http.StatusNoContent},
		{name: "Console route", path: "/console/applications", wantStatus: http.StatusNoContent},
		{name: "Console lookalike", path: "/consoles", wantStatus: http.StatusServiceUnavailable},
		{name: "translation management", path: "/i18n/languages/en-US/translations", wantStatus: http.StatusServiceUnavailable},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RestrictedModeMiddleware(true)(next)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(method, tt.path, nil))

			assert.Equal(t, tt.wantStatus, recorder.Code)
			if tt.wantStatus == http.StatusServiceUnavailable {
				assert.JSONEq(t, `{
					"code": "SSE-5030",
					"message": {
						"key": "error.operating_mode.feature_unavailable",
						"defaultValue": "Feature unavailable"
					},
					"description": {
						"key": "error.operating_mode.feature_unavailable_description",
						"defaultValue": "The requested feature is unavailable while the server is in restricted mode"
					}
				}`, recorder.Body.String())
			}
		})
	}
}

func TestRestrictedModeTranslationResolvePath(t *testing.T) {
	assert.True(t, isRestrictedModePathAllowed("/i18n/languages/en-US/translations/resolve"))
	assert.False(t, isRestrictedModePathAllowed("/i18n/languages//translations/resolve"))
	assert.False(t, isRestrictedModePathAllowed("/i18n/languages/en/extra/translations/resolve"))
}
