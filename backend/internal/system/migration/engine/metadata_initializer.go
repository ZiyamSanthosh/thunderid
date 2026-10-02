// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thunder-id/thunderid/internal/system/utils"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const preFrameworkBaselineLevel = "1.0.0"

// InitializationResult describes the state preserved or registered for one logical store.
type InitializationResult struct {
	State   State
	Created bool
}

// Initializer establishes migration metadata in one physical database namespace.
type Initializer struct {
	manifest      ReleaseManifest
	metadataStore *MetadataStore
	transactioner providers.Transactioner
	now           func() time.Time
	newID         func() (string, error)
}

// NewInitializer creates an initializer for one physical database namespace.
func NewInitializer(
	manifest ReleaseManifest,
	metadataStore *MetadataStore,
	transactioner providers.Transactioner,
) (*Initializer, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("create migration initializer: %w", err)
	}
	if metadataStore == nil {
		return nil, errors.New("create migration initializer: metadata store is required")
	}
	if transactioner == nil {
		return nil, errors.New("create migration initializer: transactioner is required")
	}

	return &Initializer{
		manifest:      manifest,
		metadataStore: metadataStore,
		transactioner: transactioner,
		now:           func() time.Time { return time.Now().UTC() },
		newID:         utils.GenerateUUIDv7,
	}, nil
}

// Initialize creates or preserves state rows for the logical stores in one physical database namespace.
func (i *Initializer) Initialize(ctx context.Context, storeIDs []StoreID) ([]InitializationResult, error) {
	requirements, err := i.requirementsFor(storeIDs)
	if err != nil {
		return nil, err
	}

	results := make([]InitializationResult, 0, len(requirements))
	err = i.transactioner.Transact(ctx, func(txCtx context.Context) error {
		schemaStatus, err := i.metadataStore.InspectSchema(txCtx)
		if err != nil {
			return err
		}
		if schemaStatus == SchemaPartial {
			return errors.New("partial metadata schema requires repair")
		}
		levelForMissingRows := make(map[StoreID]string, len(requirements))
		if schemaStatus == SchemaAbsent {
			if err := i.metadataStore.CreateSchema(txCtx); err != nil {
				return err
			}
			for _, requirement := range requirements {
				levelForMissingRows[requirement.ID] = preFrameworkBaselineLevel
			}
		} else {
			for _, requirement := range requirements {
				levelForMissingRows[requirement.ID] = requirement.RequiredLevel
			}
		}

		initializedAt := i.now()
		for _, requirement := range requirements {
			if schemaStatus == SchemaReady {
				existing, err := i.metadataStore.GetState(txCtx, requirement.ID)
				if err != nil {
					return err
				}
				if existing != nil {
					if existing.MetadataVersion != i.manifest.MigrationMetadataVersion {
						return fmt.Errorf("initialize migration metadata for store %q: metadata version %d is not supported",
							requirement.ID, existing.MetadataVersion)
					}
					results = append(results, InitializationResult{State: *existing})
					continue
				}
			}

			id, err := i.newID()
			if err != nil {
				return fmt.Errorf("initialize migration metadata for store %q: generate state ID: %w",
					requirement.ID, err)
			}
			state := State{
				ID:              id,
				StoreID:         requirement.ID,
				MetadataVersion: i.manifest.MigrationMetadataVersion,
				CompletedLevel:  levelForMissingRows[requirement.ID],
				Status:          StatusClean,
				UpdatedAt:       initializedAt,
			}
			if err := i.metadataStore.InsertState(txCtx, state); err != nil {
				return err
			}
			results = append(results, InitializationResult{State: state, Created: true})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("initialize migration metadata: %w", err)
	}
	return results, nil
}

func (i *Initializer) requirementsFor(storeIDs []StoreID) ([]StoreRequirement, error) {
	if len(storeIDs) == 0 {
		return nil, errors.New("initialize migration metadata: at least one logical store is required")
	}

	manifestStores := make(map[StoreID]StoreRequirement, len(i.manifest.Stores))
	for _, requirement := range i.manifest.Stores {
		manifestStores[requirement.ID] = requirement
	}

	requirements := make([]StoreRequirement, 0, len(storeIDs))
	seen := make(map[StoreID]struct{}, len(storeIDs))
	for _, storeID := range storeIDs {
		if _, exists := seen[storeID]; exists {
			return nil, fmt.Errorf("initialize migration metadata: duplicate store %q", storeID)
		}
		requirement, exists := manifestStores[storeID]
		if !exists {
			return nil, fmt.Errorf("initialize migration metadata: store %q is not declared by the release manifest", storeID)
		}
		seen[storeID] = struct{}{}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}
