// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyLegacySubject(t *testing.T) {
	tests := []struct {
		name string
		sub  string
		want legacySubjectProvider
	}{
		{name: "21 digit Google subject", sub: "117234981234567890123", want: legacySubjectProviderGoogle},
		{name: "20 digit Google subject", sub: "11723498123456789012", want: legacySubjectProviderGoogle},
		{name: "subject longer than 21 digits", sub: "1172349812345678901234", want: legacySubjectProviderGoogle},
		{name: "8 digit GitHub subject", sub: "12345678", want: legacySubjectProviderGitHub},
		{name: "9 digit GitHub subject", sub: "123456789", want: legacySubjectProviderGitHub},
		{name: "single digit GitHub subject", sub: "7", want: legacySubjectProviderGitHub},
		{name: "largest GitHub subject", sub: "9223372036854775807", want: legacySubjectProviderGitHub},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyLegacySubject(tt.sub)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassifyLegacySubjectRejectsUnclassifiableValues(t *testing.T) {
	tests := []struct {
		name string
		sub  string
	}{
		{name: "empty", sub: ""},
		{name: "above the largest int64", sub: "9223372036854775808"},
		{name: "19 digits above the largest int64", sub: "9999999999999999999"},
		{name: "leading zero", sub: "0123456"},
		{name: "zero", sub: "0"},
		{name: "letters", sub: "abc123"},
		{name: "Google length with a letter", sub: "11723498123456789012a"},
		{name: "surrounding whitespace", sub: " 12345678 "},
		{name: "sign", sub: "-12345678"},
		{name: "decimal point", sub: "1234.5678"},
		{name: "non ASCII digits", sub: "١٢٣٤٥٦٧٨"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyLegacySubject(tt.sub)
			require.Error(t, err)
			assert.Empty(t, got)
		})
	}
}
