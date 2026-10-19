// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/thunder-id/thunderid/internal/entitytype"
	"github.com/thunder-id/thunderid/internal/ou"
	"github.com/thunder-id/thunderid/internal/system/cache"
	"github.com/thunder-id/thunderid/internal/system/config"
	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	"github.com/thunder-id/thunderid/internal/system/migration/current"
	"github.com/thunder-id/thunderid/internal/system/security"
	"github.com/thunder-id/thunderid/internal/system/sysauthz"
)

// declarativeEntityTypeCategories are the entity type categories the migration reads.
var declarativeEntityTypeCategories = []entitytype.TypeCategory{
	entitytype.TypeCategoryUser, entitytype.TypeCategoryAgent,
}

// newDeclarativeEntityTypesProvider returns a lazy provider of the read-only entity types defined in
// declarative resource files. The service graph (organization unit service and the entity type
// service over the declarative store) is only built when an operation first asks for it, so phases
// and commands that do not need it never load the resource files.
func newDeclarativeEntityTypesProvider(cfg *config.Config) func() (current.DeclarativeEntityTypes, error) {
	return func() (current.DeclarativeEntityTypes, error) {
		security.InitSystemPermissions(cfg.Server.SecurityConfig.SystemPermissionPrefix)
		authzService, err := sysauthz.Initialize()
		if err != nil {
			return nil, fmt.Errorf("initialize system authorization: %w", err)
		}
		cacheManager := cache.Initialize(cfg.Cache, cfg.Server.Identifier)
		ouService, err := ou.InitializeService(cacheManager, authzService)
		if err != nil {
			return nil, fmt.Errorf("initialize organization unit service: %w", err)
		}
		service, err := entitytype.InitializeDeclarativeService(ouService, authzService)
		if err != nil {
			return nil, fmt.Errorf("initialize declarative entity type service: %w", err)
		}
		return declarativeEntityTypeLister{service: service}, nil
	}
}

type declarativeEntityTypeLister struct {
	service entitytype.EntityTypeServiceInterface
}

// ListDeclarativeEntityTypes lists every entity type the declarative store holds, for all categories.
func (l declarativeEntityTypeLister) ListDeclarativeEntityTypes(
	ctx context.Context,
) ([]current.DeclarativeEntityType, error) {
	runtimeCtx := security.WithRuntimeContext(ctx)
	var result []current.DeclarativeEntityType
	for _, category := range declarativeEntityTypeCategories {
		for offset := 0; ; offset += serverconst.MaxPageSize {
			page, svcErr := l.service.GetEntityTypeList(
				runtimeCtx, category, serverconst.MaxPageSize, offset, false)
			if svcErr != nil {
				return nil, fmt.Errorf("list %s entity types: %s", category, svcErr.ErrorDescription)
			}
			// The service is built over the declarative store only, so every entry is declarative.
			// IsReadOnly is not set here: only the composite store populates it.
			for _, item := range page.Types {
				supportsNameAttribute := false
				if category == entitytype.TypeCategoryAgent {
					entityType, svcErr := l.service.GetEntityTypeSchema(runtimeCtx, category, item.Handle)
					if svcErr != nil {
						return nil, fmt.Errorf("read declarative %s entity type %q: %s",
							category, item.Handle, svcErr.ErrorDescription)
					}
					var err error
					supportsNameAttribute, err = declarativeEntityTypeSupportsName(entityType.Schema)
					if err != nil {
						return nil, fmt.Errorf("inspect declarative %s entity type %q: %w",
							category, item.Handle, err)
					}
				}
				result = append(result, current.DeclarativeEntityType{
					Category: string(category), Handle: item.Handle, DisplayName: item.DisplayName,
					SupportsNameAttribute: supportsNameAttribute,
				})
			}
			if len(page.Types) < serverconst.MaxPageSize {
				break
			}
		}
	}
	return result, nil
}

func declarativeEntityTypeSupportsName(schema json.RawMessage) (bool, error) {
	var attributes map[string]struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema, &attributes); err != nil {
		return false, err
	}
	name, exists := attributes["name"]
	return exists && name.Type == "string", nil
}
