// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strings"
)

// QueryRow provides safe typed access to one migration query result row.
type QueryRow map[string]interface{}

// Bool returns a required boolean column.
func (r QueryRow) Bool(column string) (bool, error) {
	value, exists := r[column]
	if !exists || value == nil {
		return false, fmt.Errorf("column %q is missing", column)
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("column %q has unsupported type %T", column, value)
	}
	return result, nil
}

// String returns a required non-empty string column.
func (r QueryRow) String(column string) (string, error) {
	value, exists := r[column]
	if !exists || value == nil {
		return "", fmt.Errorf("column %q is missing", column)
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return "", fmt.Errorf("column %q is empty", column)
		}
		return typed, nil
	case []byte:
		if len(typed) == 0 {
			return "", fmt.Errorf("column %q is empty", column)
		}
		return string(typed), nil
	default:
		return "", fmt.Errorf("column %q has unsupported type %T", column, value)
	}
}

// Int64 returns a required integer column normalized to int64.
func (r QueryRow) Int64(column string) (int64, error) {
	value, exists := r[column]
	if !exists || value == nil {
		return 0, fmt.Errorf("column %q is missing", column)
	}
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	default:
		return 0, fmt.Errorf("column %q has unsupported type %T", column, value)
	}
}
