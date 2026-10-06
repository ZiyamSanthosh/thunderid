// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thunder-id/thunderid/internal/system/utils"
)

const (
	maxFailureSummaryLength = 1024
	maxOperationIDLength    = 255
	maxCheckpointSize       = 64 * 1024
)

// Status represents a migration state or operation status.
type Status string

const (
	// StatusClean means that a logical store is ready to serve at its completed level.
	StatusClean Status = "clean"
	// StatusInProgress means that a migration run or operation has started but has not completed.
	StatusInProgress Status = "in_progress"
	// StatusCompleted means that a migration operation completed successfully.
	StatusCompleted Status = "completed"
	// StatusFailed means that a migration run or operation stopped after an error.
	StatusFailed Status = "failed"
)

// MigrationStage identifies the durable lifecycle position of one logical store migration.
type MigrationStage string

const (
	// MigrationStageComplete means that no release migration is active for the store.
	MigrationStageComplete MigrationStage = "complete"
	// MigrationStageExpanding means that compatible structures are being prepared.
	MigrationStageExpanding MigrationStage = "expanding"
	// MigrationStageExpanded means that expansion completed and migration has not started.
	MigrationStageExpanded MigrationStage = "expanded"
	// MigrationStageMigrating means that data transformation and catch-up are running.
	MigrationStageMigrating MigrationStage = "migrating"
	// MigrationStageContractReady means that target serving is ready and contract awaits explicit execution.
	MigrationStageContractReady MigrationStage = "contract_ready"
	// MigrationStageContracting means that explicitly initiated cleanup is running.
	MigrationStageContracting MigrationStage = "contracting"
)

// State is the durable compatibility state of one logical store.
type State struct {
	ID              string
	StoreID         StoreID
	MetadataVersion int
	CompletedLevel  string
	TargetLevel     string
	Stage           MigrationStage
	Status          Status
	ActiveRunID     string
	FailureSummary  string
	UpdatedAt       time.Time
}

// Validate verifies that the state can be persisted by the migration engine.
func (s State) Validate() error {
	if !isUUIDv7(s.ID) {
		return errors.New("migration state ID must be a canonical UUIDv7")
	}
	if !isSupportedStoreID(s.StoreID) {
		return fmt.Errorf("unsupported migration state store %q", s.StoreID)
	}
	if s.MetadataVersion < 1 {
		return errors.New("migration state metadata version must be positive")
	}
	if !isCanonicalVersion(s.CompletedLevel) {
		return fmt.Errorf("migration state completed level %q is not canonical semantic version", s.CompletedLevel)
	}
	if s.UpdatedAt.IsZero() {
		return errors.New("migration state update time is required")
	}
	if utf8.RuneCountInString(s.FailureSummary) > maxFailureSummaryLength {
		return fmt.Errorf("migration state failure summary exceeds %d characters", maxFailureSummaryLength)
	}

	switch s.Status {
	case StatusClean:
		if s.Stage != MigrationStageComplete {
			return errors.New("clean migration state must be at the complete stage")
		}
		if s.TargetLevel != "" || s.ActiveRunID != "" || s.FailureSummary != "" {
			return errors.New("clean migration state cannot contain an active target, run or failure")
		}
	case StatusInProgress:
		if !s.Stage.executing() {
			return errors.New("in-progress migration state requires an executing stage")
		}
		if err := s.validateActiveRun(); err != nil {
			return err
		}
		if s.FailureSummary != "" {
			return errors.New("in-progress migration state cannot contain a failure summary")
		}
	case StatusFailed:
		if !s.Stage.executing() {
			return errors.New("failed migration state requires an executing stage")
		}
		if err := s.validateActiveRun(); err != nil {
			return err
		}
		if strings.TrimSpace(s.FailureSummary) == "" {
			return errors.New("failed migration state requires a failure summary")
		}
	case StatusCompleted:
		if s.Stage != MigrationStageExpanded && s.Stage != MigrationStageContractReady {
			return errors.New("completed migration state requires an expanded or contract-ready stage")
		}
		if err := s.validatePendingTarget(); err != nil {
			return err
		}
		if s.ActiveRunID != "" || s.FailureSummary != "" {
			return errors.New("completed migration state cannot contain an active run or failure")
		}
	default:
		return fmt.Errorf("unsupported migration state status %q", s.Status)
	}

	return nil
}

func (s State) validatePendingTarget() error {
	if !isCanonicalVersion(s.TargetLevel) {
		return fmt.Errorf("migration state target level %q is not canonical semantic version", s.TargetLevel)
	}
	if s.TargetLevel == s.CompletedLevel {
		return errors.New("migration state target level must differ from completed level")
	}
	return nil
}

func (s State) validateActiveRun() error {
	if err := s.validatePendingTarget(); err != nil {
		return err
	}
	if !isUUIDv7(s.ActiveRunID) {
		return errors.New("migration state active run ID must be a canonical UUIDv7")
	}
	return nil
}

func (s MigrationStage) executing() bool {
	return s == MigrationStageExpanding || s == MigrationStageMigrating || s == MigrationStageContracting
}

// Progress is the durable execution state of one migration operation.
type Progress struct {
	ID               string
	MigrationStateID string
	RunID            string
	TargetLevel      string
	OperationID      string
	OperationOrder   int
	Status           Status
	CheckpointData   json.RawMessage
	UpdatedAt        time.Time
}

// Validate verifies that the operation progress can be persisted by the migration engine.
func (p Progress) Validate() error {
	if !isUUIDv7(p.ID) {
		return errors.New("migration progress ID must be a canonical UUIDv7")
	}
	if !isUUIDv7(p.MigrationStateID) {
		return errors.New("migration progress state ID must be a canonical UUIDv7")
	}
	if !isUUIDv7(p.RunID) {
		return errors.New("migration progress run ID must be a canonical UUIDv7")
	}
	if !isCanonicalVersion(p.TargetLevel) {
		return fmt.Errorf("migration progress target level %q is not canonical semantic version", p.TargetLevel)
	}
	if strings.TrimSpace(p.OperationID) == "" {
		return errors.New("migration progress operation ID is required")
	}
	if utf8.RuneCountInString(p.OperationID) > maxOperationIDLength {
		return fmt.Errorf("migration progress operation ID exceeds %d characters", maxOperationIDLength)
	}
	if p.OperationOrder < 0 {
		return errors.New("migration progress operation order cannot be negative")
	}
	if p.Status != StatusInProgress && p.Status != StatusCompleted && p.Status != StatusFailed {
		return fmt.Errorf("unsupported migration progress status %q", p.Status)
	}
	if len(p.CheckpointData) > maxCheckpointSize {
		return fmt.Errorf("migration progress checkpoint exceeds %d bytes", maxCheckpointSize)
	}
	if len(p.CheckpointData) > 0 && !json.Valid(p.CheckpointData) {
		return errors.New("migration progress checkpoint must be valid JSON")
	}
	if p.UpdatedAt.IsZero() {
		return errors.New("migration progress update time is required")
	}
	return nil
}

func isSupportedStoreID(storeID StoreID) bool {
	for _, supportedStoreID := range supportedStoreIDs {
		if storeID == supportedStoreID {
			return true
		}
	}
	return false
}

func isUUIDv7(value string) bool {
	return utils.IsValidUUID(value) && strings.ToLower(value) == value && value[14] == '7' &&
		strings.ContainsRune("89ab", rune(value[19]))
}
