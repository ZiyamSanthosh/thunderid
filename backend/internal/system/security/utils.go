// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"fmt"
	"regexp"
	"strings"
)

// compiledAPIPermission holds the pre-compiled regex form of a single apiPermissionEntry.
type compiledAPIPermission struct {
	re         *regexp.Regexp
	permission string
}

// compilePathPattern compiles a single glob-style path pattern into a regular expression.
// It returns an error if the pattern is invalid.
//
// Supported syntax:
//   - "*"  matches exactly one path segment (no slashes).
//   - "**" matches zero or more path segments; only valid as the suffix after "/" (e.g., "/a/**").
func compilePathPattern(pattern string) (*regexp.Regexp, error) {
	var regexPattern string

	if strings.Contains(pattern, "**") {
		// Ensure "**" is only used as a suffix "/**"
		if !strings.HasSuffix(pattern, "/**") {
			return nil,
				fmt.Errorf("invalid pattern: recursive wildcard '**' is only allowed as a suffix: %s", pattern)
		}
		// Ensure "**" appears only once
		if strings.Count(pattern, "**") > 1 {
			return nil, fmt.Errorf("invalid pattern: recursive wildcard '**' can only appear once: %s", pattern)
		}
		base := strings.TrimSuffix(pattern, "/**")
		baseRegex := regexp.QuoteMeta(base)
		baseRegex = strings.ReplaceAll(baseRegex, "\\*", "[^/]+")
		regexPattern = "^" + baseRegex + "(?:/.*)?$"
	} else {
		// Normal pattern (no recursive wildcards)
		regexPattern = regexp.QuoteMeta(pattern)
		regexPattern = strings.ReplaceAll(regexPattern, "\\*", "[^/]+")
		regexPattern = "^" + regexPattern + "$"
	}

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil, fmt.Errorf("error compiling path pattern regex for pattern %s: %w", pattern, err)
	}
	return re, nil
}

// PathMatcher matches request paths against glob-style path patterns, using the same syntax as the
// public path patterns of the security middleware.
type PathMatcher struct {
	patterns []*regexp.Regexp
}

// NewPathMatcher compiles the given glob-style path patterns into a PathMatcher.
// It returns an error if any pattern is invalid.
func NewPathMatcher(patterns ...string) (*PathMatcher, error) {
	compiled, err := compilePathPatterns(patterns)
	if err != nil {
		return nil, err
	}
	return &PathMatcher{patterns: compiled}, nil
}

// MustNewPathMatcher is like NewPathMatcher but panics if any pattern is invalid. It is intended for
// patterns that are fixed at compile time.
func MustNewPathMatcher(patterns ...string) *PathMatcher {
	matcher, err := NewPathMatcher(patterns...)
	if err != nil {
		panic(err)
	}
	return matcher
}

// Matches reports whether the path matches any of the matcher's patterns.
func (m *PathMatcher) Matches(path string) bool {
	if len(path) > maxPublicPathLength {
		return false
	}
	for _, re := range m.patterns {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// compilePathPatterns compiles a slice of glob-style path patterns into regular expressions.
// It returns an error if any pattern is invalid.
func compilePathPatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := compilePathPattern(pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// compileAPIPermissions compiles a slice of apiPermissionEntry values into their regex form.
// It returns an error if any pattern is invalid.
func compileAPIPermissions(entries []apiPermissionEntry) ([]compiledAPIPermission, error) {
	compiled := make([]compiledAPIPermission, 0, len(entries))
	for _, entry := range entries {
		re, err := compilePathPattern(entry.pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, compiledAPIPermission{re: re, permission: entry.permission})
	}
	return compiled, nil
}
