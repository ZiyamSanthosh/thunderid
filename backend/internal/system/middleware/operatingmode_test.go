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
		{name: "OpenID4VP endpoint", path: "/openid4vp/request", wantStatus: http.StatusNoContent},
		{name: "application management", path: "/applications/example", wantStatus: http.StatusServiceUnavailable},
		{name: "user management", path: "/users", wantStatus: http.StatusServiceUnavailable},
		{name: "dynamic client registration", path: "/oauth2/dcr/register", wantStatus: http.StatusServiceUnavailable},
		{name: "passkey registration", path: "/register/passkey/start", wantStatus: http.StatusServiceUnavailable},
		{name: "Console asset", path: "/console/index.html", wantStatus: http.StatusServiceUnavailable},
		{name: "translation management", path: "/i18n/languages/en-US/translations", wantStatus: http.StatusServiceUnavailable},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RestrictedModeMiddleware(true)(next)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assert.Equal(t, tt.wantStatus, recorder.Code)
			if tt.wantStatus == http.StatusServiceUnavailable {
				assert.JSONEq(t, `{
					"error": "feature_unavailable",
					"error_description": "The requested feature is unavailable while ThunderID is in restricted mode."
				}`, recorder.Body.String())
			}
		})
	}
}

func TestIsTranslationResolvePath(t *testing.T) {
	assert.True(t, isTranslationResolvePath("/i18n/languages/en-US/translations/resolve"))
	assert.False(t, isTranslationResolvePath("/i18n/languages//translations/resolve"))
	assert.False(t, isTranslationResolvePath("/i18n/languages/en/extra/translations/resolve"))
}
