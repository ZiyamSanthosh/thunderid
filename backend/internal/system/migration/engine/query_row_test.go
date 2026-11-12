// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryRowBool(t *testing.T) {
	boolValue, err := (QueryRow{"value": true}).Bool("value")
	require.NoError(t, err)
	assert.True(t, boolValue)

	_, missingErr := (QueryRow{}).Bool("value")
	_, invalidErr := (QueryRow{"value": "true"}).Bool("value")
	require.Error(t, missingErr)
	require.Error(t, invalidErr)
}

func TestQueryRowString(t *testing.T) {
	stringValue, err := (QueryRow{"value": []byte("stored")}).String("value")
	require.NoError(t, err)
	assert.Equal(t, "stored", stringValue)

	_, missingErr := (QueryRow{}).String("value")
	_, emptyErr := (QueryRow{"value": " "}).String("value")
	_, invalidErr := (QueryRow{"value": 1}).String("value")
	require.Error(t, missingErr)
	require.Error(t, emptyErr)
	require.Error(t, invalidErr)
}

func TestQueryRowInt64(t *testing.T) {
	intValue, err := (QueryRow{"value": int(30)}).Int64("value")
	require.NoError(t, err)
	assert.Equal(t, int64(30), intValue)

	int32Value, err := (QueryRow{"value": int32(30)}).Int64("value")
	require.NoError(t, err)
	assert.Equal(t, int64(30), int32Value)

	_, missingErr := (QueryRow{}).Int64("value")
	_, invalidErr := (QueryRow{"value": "30"}).Int64("value")
	require.Error(t, missingErr)
	require.Error(t, invalidErr)
}
