// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"fmt"
	"regexp"
	"strconv"
)

// legacySubjectProvider names the federated connection a legacy subject belongs to. The values match the
// type of the corresponding declarative connection.
type legacySubjectProvider string

const (
	legacySubjectProviderGoogle legacySubjectProvider = "google"
	legacySubjectProviderGitHub legacySubjectProvider = "github"
)

var (
	// Google subjects are decimal strings of 21 digits, or 20 for older accounts.
	googleLegacySubjectPattern = regexp.MustCompile(`^[0-9]{20,}$`)
	// GitHub subjects are positive account IDs that fit in an int64, without leading zeros.
	githubLegacySubjectPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
)

// classifyLegacySubject decides which connection issued a subject that was stored before federated
// identities were recorded as links. The two ranges do not overlap. A subject that fits neither is an
// error and must not be linked, because a guess would attach the user to the wrong connection.
func classifyLegacySubject(sub string) (legacySubjectProvider, error) {
	if googleLegacySubjectPattern.MatchString(sub) {
		return legacySubjectProviderGoogle, nil
	}
	if githubLegacySubjectPattern.MatchString(sub) {
		if _, err := strconv.ParseInt(sub, 10, 64); err != nil {
			return "", fmt.Errorf("subject has %d digits but exceeds the largest GitHub account ID", len(sub))
		}
		return legacySubjectProviderGitHub, nil
	}
	return "", fmt.Errorf("subject of %d characters matches neither the Google nor the GitHub format", len(sub))
}
