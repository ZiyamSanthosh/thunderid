// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCompilePathPattern verifies that individual glob-style patterns are compiled
// to the correct anchored regular expression, and that invalid patterns are rejected.
func TestCompilePathPattern(t *testing.T) {
	tests := []struct {
		name           string
		pattern        string
		expectedRegex  string
		shouldMatch    []string
		shouldNotMatch []string
	}{
		{
			name:           "Exact path",
			pattern:        "/users/me",
			expectedRegex:  "^/users/me$",
			shouldMatch:    []string{"/users/me"},
			shouldNotMatch: []string{"/users/menu", "/users/me/profile", "/users"},
		},
		{
			name:           "Single wildcard segment",
			pattern:        "/api/*/users",
			expectedRegex:  "^/api/[^/]+/users$",
			shouldMatch:    []string{"/api/v1/users", "/api/test/users"},
			shouldNotMatch: []string{"/api/users", "/api/v1/v2/users"},
		},
		{
			name:           "Recursive wildcard suffix",
			pattern:        "/health/**",
			expectedRegex:  "^/health(?:/.*)?$",
			shouldMatch:    []string{"/health", "/health/", "/health/liveness", "/health/readiness/full"},
			shouldNotMatch: []string{"/healthz", "/other"},
		},
		{
			name:           "Multiple single wildcards",
			pattern:        "/i18n/languages/*/translations/ns/*/keys/*/resolve",
			expectedRegex:  "^/i18n/languages/[^/]+/translations/ns/[^/]+/keys/[^/]+/resolve$",
			shouldMatch:    []string{"/i18n/languages/en/translations/ns/common/keys/btn.submit/resolve"},
			shouldNotMatch: []string{"/i18n/languages/en/translations/ns/common/keys/btn.submit/extra"},
		},
		{
			name:           "Special characters escaped",
			pattern:        "/api/v1.0/user",
			expectedRegex:  "^/api/v1\\.0/user$",
			shouldMatch:    []string{"/api/v1.0/user"},
			shouldNotMatch: []string{"/api/v1a0/user"},
		},
		{
			name:           "Invalid: globstar in middle",
			pattern:        "/api/**/users",
			expectedRegex:  "",
			shouldMatch:    nil,
			shouldNotMatch: nil,
		},
		{
			name:           "Invalid: multiple globstars",
			pattern:        "/api/**/users/**",
			expectedRegex:  "",
			shouldMatch:    nil,
			shouldNotMatch: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re, err := compilePathPattern(tt.pattern)

			if tt.expectedRegex == "" {
				assert.Error(t, err)
				assert.Nil(t, re)
				assert.Contains(t, err.Error(), "invalid pattern")
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, re)
				assert.Equal(t, tt.expectedRegex, re.String())

				for _, matchPath := range tt.shouldMatch {
					assert.True(t, re.MatchString(matchPath), "Should match: %s", matchPath)
				}
				for _, mismatchPath := range tt.shouldNotMatch {
					assert.False(t, re.MatchString(mismatchPath), "Should not match: %s", mismatchPath)
				}
			}
		})
	}
}

// TestCompilePathPatterns verifies the batch wrapper: it returns the correct
// count of compiled patterns and stops at the first invalid entry.
func TestCompilePathPatterns(t *testing.T) {
	tests := []struct {
		name        string
		patterns    []string
		wantLen     int
		wantError   bool
		errContains string
	}{
		{
			name:     "Empty slice",
			patterns: []string{},
			wantLen:  0,
		},
		{
			name:     "All valid patterns",
			patterns: []string{"/health/**", "/api/*/resource", "/exact"},
			wantLen:  3,
		},
		{
			name:        "First pattern invalid",
			patterns:    []string{"/invalid/**/middle/**", "/valid/**"},
			wantError:   true,
			errContains: "invalid pattern",
		},
		{
			name:        "Last pattern invalid",
			patterns:    []string{"/valid/**", "/invalid/**/middle/**"},
			wantError:   true,
			errContains: "invalid pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := compilePathPatterns(tt.patterns)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, compiled)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
				assert.Len(t, compiled, tt.wantLen)
			}
		})
	}
}

// TestCompileAPIPermissions verifies that API permission entries are compiled
// to regex form correctly, and that invalid patterns are rejected.
func TestCompileAPIPermissions(t *testing.T) {
	tests := []struct {
		name        string
		entries     []apiPermissionEntry
		wantLen     int
		wantError   bool
		errContains string
	}{
		{
			name:    "Empty slice",
			entries: []apiPermissionEntry{},
			wantLen: 0,
		},
		{
			name: "Valid entries compiled",
			entries: []apiPermissionEntry{
				{"GET /users", "system:user:view"},
				{"GET /users/**", "system:user:view"},
				{"POST /users", "system:user"},
			},
			wantLen: 3,
		},
		{
			name: "Single wildcard entry",
			entries: []apiPermissionEntry{
				{"GET /users/*/profile", "system:user:view"},
			},
			wantLen: 1,
		},
		{
			name: "Invalid pattern stops compilation",
			entries: []apiPermissionEntry{
				{"GET /valid/**", "system:user:view"},
				{"GET /invalid/**/middle/**", "system:user"},
			},
			wantError:   true,
			errContains: "invalid pattern",
		},
		{
			name: "Invalid pattern as first entry",
			entries: []apiPermissionEntry{
				{"GET /invalid/**/middle/**", "system:user"},
				{"GET /valid/**", "system:user:view"},
			},
			wantError:   true,
			errContains: "invalid pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := compileAPIPermissions(tt.entries)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, compiled)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
				assert.Len(t, compiled, tt.wantLen)
			}
		})
	}
}

func TestPathMatcher(t *testing.T) {
	matcher, err := NewPathMatcher("/ou/*/oauth2/token", "/gate/**")
	assert.NoError(t, err)

	assert.True(t, matcher.Matches("/ou/child/oauth2/token"))
	assert.True(t, matcher.Matches("/gate"))
	assert.True(t, matcher.Matches("/gate/assets/main.js"))
	assert.False(t, matcher.Matches("/ou/a/b/oauth2/token"))
	assert.False(t, matcher.Matches("/oauth2/token"))
	assert.False(t, matcher.Matches("/"+strings.Repeat("a", maxPublicPathLength)))
}

func TestNewPathMatcher_InvalidPattern(t *testing.T) {
	matcher, err := NewPathMatcher("/a/**/b")
	assert.Error(t, err)
	assert.Nil(t, matcher)
	assert.Panics(t, func() { MustNewPathMatcher("/a/**/b") })
}
