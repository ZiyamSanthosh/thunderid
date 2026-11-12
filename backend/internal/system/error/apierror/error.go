// Copyright 2025 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package apierror defines the error structures for the API.
package apierror

import (
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

// ErrorResponse defines an API error response with i18n support.
type ErrorResponse struct {
	Code        string                `json:"code"`
	Message     tidcommon.I18nMessage `json:"message"`
	Description tidcommon.I18nMessage `json:"description"`
}

// Operating mode error responses.
var (
	// ErrFeatureUnavailable is returned when the server's operating mode does not serve the request (HTTP 503).
	ErrFeatureUnavailable = ErrorResponse{
		Code: "SSE-5030",
		Message: tidcommon.I18nMessage{
			Key:          "error.operating_mode.feature_unavailable",
			DefaultValue: "Feature unavailable",
		},
		Description: tidcommon.I18nMessage{
			Key:          "error.operating_mode.feature_unavailable_description",
			DefaultValue: "The requested feature is unavailable while the server is in restricted mode",
		},
	}
)

// Authentication and authorization error responses, returned by the security middleware.
var (
	// ErrUnauthorized is returned when authentication credentials are missing or invalid (HTTP 401).
	ErrUnauthorized = ErrorResponse{
		Code: "AUTH-4010",
		Message: tidcommon.I18nMessage{
			Key:          "error.auth.unauthorized",
			DefaultValue: "Unauthorized",
		},
		Description: tidcommon.I18nMessage{
			Key:          "error.auth.unauthorized_description",
			DefaultValue: "Authentication is required to access this resource",
		},
	}

	// ErrForbidden is returned when the caller is authenticated but lacks sufficient permissions (HTTP 403).
	ErrForbidden = ErrorResponse{
		Code: "AUTH-4030",
		Message: tidcommon.I18nMessage{
			Key:          "error.auth.forbidden",
			DefaultValue: "Forbidden",
		},
		Description: tidcommon.I18nMessage{
			Key:          "error.auth.forbidden_description",
			DefaultValue: "You do not have sufficient permissions to access this resource",
		},
	}
)
