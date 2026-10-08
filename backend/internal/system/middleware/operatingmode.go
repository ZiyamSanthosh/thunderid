// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
)

const (
	featureUnavailableErrorCode        = "feature_unavailable"
	featureUnavailableErrorDescription = "The requested feature is unavailable while ThunderID is in restricted mode."
)

var restrictedModeExactPaths = map[string]struct{}{
	"/.well-known/oauth-authorization-server": {},
	"/.well-known/openid-configuration":       {},
	"/design/resolve":                         {},
	"/flow/execute":                           {},
	"/flow/meta":                              {},
	"/gate":                                   {},
	"/oauth2/auth/callback":                   {},
	"/oauth2/authorize":                       {},
	"/oauth2/bc-authorize":                    {},
	"/oauth2/bc-authorize/callback":           {},
	"/oauth2/introspect":                      {},
	"/oauth2/jwks":                            {},
	"/oauth2/logout":                          {},
	"/oauth2/logout/callback":                 {},
	"/oauth2/par":                             {},
	"/oauth2/revoke":                          {},
	"/oauth2/token":                           {},
	"/oauth2/userinfo":                        {},
}

var restrictedModePathPrefixes = []string{
	"/gate/",
	"/health/",
	"/openid4vp/",
}

// RestrictedModeMiddleware restricts the HTTP surface to the endpoints allowed in restricted mode.
func RestrictedModeMiddleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isRestrictedModePathAllowed(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			writeFeatureUnavailableError(w)
		})
	}
}

func isRestrictedModePathAllowed(path string) bool {
	if _, ok := restrictedModeExactPaths[path]; ok {
		return true
	}

	for _, prefix := range restrictedModePathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return isTranslationResolvePath(path)
}

func isTranslationResolvePath(path string) bool {
	const (
		prefix = "/i18n/languages/"
		suffix = "/translations/resolve"
	)

	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}

	language := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return language != "" && !strings.Contains(language, "/")
}

func writeFeatureUnavailableError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             featureUnavailableErrorCode,
		"error_description": featureUnavailableErrorDescription,
	})
}
