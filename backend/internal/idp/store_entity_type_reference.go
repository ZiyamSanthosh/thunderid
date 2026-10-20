// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package idp

import (
	"context"
	"fmt"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// normalizeEntityTypeReferences rewrites the user types named in the attribute configurations to entity
// type handles. Identity providers stored before entity types had handles name the user types. They appear
// as the default user type, as the values of the value mapping, and on the user type attribute mappings.
// The configurations are handled together so that a single lookup serves them all.
func (s *idpStore) normalizeEntityTypeReferences(
	ctx context.Context, configurations ...*providers.AttributeConfiguration,
) error {
	if s.entityTypeNormalizer == nil {
		return nil
	}

	var userTypes []string
	for _, configuration := range configurations {
		visitUserTypeReferences(configuration, func(userType *string) {
			if *userType != "" {
				userTypes = append(userTypes, *userType)
			}
		})
	}
	if len(userTypes) == 0 {
		return nil
	}

	handles, err := s.entityTypeNormalizer.NormalizeEntityTypeReferences(ctx, entitytype.TypeCategoryUser, userTypes)
	if err != nil {
		return fmt.Errorf("failed to normalize entity type references: %w", err)
	}

	for _, configuration := range configurations {
		visitUserTypeReferences(configuration, func(userType *string) {
			if handle, found := handles[*userType]; found {
				*userType = handle
			}
		})
	}
	return nil
}

// visitUserTypeReferences calls visit with a pointer to every user type reference in the configuration.
func visitUserTypeReferences(configuration *providers.AttributeConfiguration, visit func(userType *string)) {
	if configuration == nil {
		return
	}
	if resolution := configuration.UserTypeResolution; resolution != nil {
		visit(&resolution.Default)
		for key, userType := range resolution.ValueMapping {
			visit(&userType)
			resolution.ValueMapping[key] = userType
		}
	}
	for i := range configuration.UserTypeAttributeMappings {
		visit(&configuration.UserTypeAttributeMappings[i].UserType)
	}
}
