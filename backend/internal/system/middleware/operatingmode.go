// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"

	"github.com/thunder-id/thunderid/internal/system/error/apierror"
	"github.com/thunder-id/thunderid/internal/system/security"
	"github.com/thunder-id/thunderid/internal/system/utils"
)

// restrictedModePaths are the paths served in restricted mode. They use the glob syntax of the security
// middleware's public paths, where "*" matches one segment and a trailing "/**" matches the base path and
// everything below it.
var restrictedModePaths = security.MustNewPathMatcher(
	"/.well-known/oauth-authorization-server",
	"/.well-known/openid-configuration",
	"/console/**",
	"/design/resolve",
	"/flow/execute",
	"/flow/meta",
	"/gate/**",
	"/health/**",
	"/i18n/languages/*/translations/resolve",
	"/oauth2/auth/callback",
	"/oauth2/authorize",
	"/oauth2/bc-authorize",
	"/oauth2/bc-authorize/callback",
	"/oauth2/introspect",
	"/oauth2/jwks",
	"/oauth2/logout",
	"/oauth2/logout/callback",
	"/oauth2/par",
	"/oauth2/revoke",
	"/oauth2/token",
	"/oauth2/userinfo",
	"/openid4vp/initiate",
	"/openid4vp/request",
	"/openid4vp/response",
	"/openid4vp/status/**",
	"/ou/*/oauth2/token",
)

// restrictedModeReadOnlyPaths are the management resources that stay readable in restricted mode,
// so that their data can be inspected while a migration is in progress. Only read methods are allowed on
// them, and everything else on these paths is still unavailable.
var restrictedModeReadOnlyPaths = security.MustNewPathMatcher(
	"/agent-types/**",
	"/agents/**",
	"/applications/**",
	"/connections/**",
	"/flows/**",
	"/gateways/**",
	"/groups/**",
	"/organization-units/**",
	"/resource-servers/**",
	"/roles/**",
	"/user-types/**",
	"/users/**",
)

// RestrictedModeMiddleware restricts the HTTP surface to the endpoints allowed in restricted mode.
func RestrictedModeMiddleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isRestrictedModePathAllowed(r.URL.Path) || isRestrictedModeReadAllowed(r.Method, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			utils.WriteErrorResponse(r.Context(), w, http.StatusServiceUnavailable, apierror.ErrFeatureUnavailable)
		})
	}
}

func isRestrictedModePathAllowed(path string) bool {
	return restrictedModePaths.Matches(path)
}

// isRestrictedModeReadAllowed reports whether the request reads one of the management resources that stay
// readable in restricted mode. OPTIONS is allowed so that browser clients can complete CORS preflight.
func isRestrictedModeReadAllowed(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return false
	}

	return restrictedModeReadOnlyPaths.Matches(path)
}
