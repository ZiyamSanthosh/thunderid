// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationQueryIDsAreUnique(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	queryID := regexp.MustCompile(`\bID:\s+"(MIG-[A-Z_-]+-?[0-9]+(?:-[0-9]+)?)"`)
	seen := make(map[string]string)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, match := range queryID.FindAllStringSubmatch(string(source), -1) {
			previous, duplicate := seen[match[1]]
			assert.Falsef(t, duplicate, "query ID %s is declared in both %s and %s", match[1], previous, file)
			seen[match[1]] = file
		}
	}
	assert.NotEmpty(t, seen)
}
