// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

// sessionState describes the runtime persistent session schema a test wants the fake database to report.
type sessionState struct {
	relations          map[string]string
	legacyPrimaryKeys  map[string]string
	typeColumn         []engine.QueryRow
	indexes            []engine.QueryRow
	checks             []engine.QueryRow
	subjectColumns     []engine.QueryRow
	subjectIndex       []engine.QueryRow
	cleanupReferences  []engine.QueryRow
	participantColumns []engine.QueryRow
	err                error
}

type sessionAccess struct {
	state sessionState
}

func (a sessionAccess) Query(
	_ context.Context,
	storeID engine.StoreID,
	query dbmodel.DBQuery,
	args ...interface{},
) ([]engine.QueryRow, error) {
	if a.state.err != nil {
		return nil, a.state.err
	}
	if storeID != engine.StoreIDRuntimePersistent {
		return nil, errors.New("unexpected store")
	}
	switch query.GetID() {
	case querySessionRelations.GetID():
		rows := make([]engine.QueryRow, 0, len(a.state.relations))
		for name, kind := range a.state.relations {
			rows = append(rows, engine.QueryRow{"relation_name": name, "relation_kind": kind})
		}
		return rows, nil
	case querySessionPrimaryKey.GetID():
		columns, ok := a.state.legacyPrimaryKeys[args[0].(string)]
		if !ok {
			return nil, nil
		}
		return []engine.QueryRow{{"column_names": columns}}, nil
	case querySessionTypeColumn.GetID():
		return a.state.typeColumn, nil
	case querySessionIndexNames.GetID():
		return a.state.indexes, nil
	case querySessionCheckConstraints.GetID():
		return a.state.checks, nil
	case querySessionSubjectColumns.GetID():
		return a.state.subjectColumns, nil
	case querySessionSubjectIndex.GetID():
		return a.state.subjectIndex, nil
	case querySessionCleanupReferences.GetID():
		return a.state.cleanupReferences, nil
	case queryTableColumns.GetID():
		return a.state.participantColumns, nil
	}
	return nil, errors.New("unexpected query " + query.GetID())
}

func (sessionAccess) DatabaseType(engine.StoreID) (engine.DatabaseType, error) {
	return engine.DatabaseTypePostgres, nil
}

func sessionTypeColumnRow(nullable string, dataType string, length int64, hasDefault string) []engine.QueryRow {
	return []engine.QueryRow{{
		"is_nullable": nullable, "data_type": dataType, "max_length": length, "has_default": hasDefault,
	}}
}

func sessionTypeCheckRows(validated bool, definition string) []engine.QueryRow {
	return []engine.QueryRow{{
		"constraint_name": sessionTypeCheckConstraint, "is_validated": validated, "definition": definition,
	}}
}

func sessionIndexRows(names ...string) []engine.QueryRow {
	var rows []engine.QueryRow
	for _, name := range names {
		table := sessionTable
		if name == "SESSION_PARTICIPANT_pkey" || name == "SSO_SESSION_PARTICIPANT_pkey" ||
			name == "idx_session_participant_app" || name == "idx_sso_session_participant_app" {
			table = sessionParticipantTable
		}
		rows = append(rows, engine.QueryRow{"table_name": table, "index_name": name})
	}
	return rows
}

const sessionTypeCheckDefinition = `CHECK (((type)::text = ANY ((ARRAY['SSO'::character varying, ` +
	`'DIRECT'::character varying])::text[])))`

var (
	legacyIndexNames = []string{
		"SSO_SESSION_pkey", "idx_sso_session_handle_id", "idx_sso_session_flow_execution",
		"idx_sso_session_absolute_expires_at", "SSO_SESSION_PARTICIPANT_pkey", "idx_sso_session_participant_app",
	}
	renamedIndexNames = []string{
		"SESSION_pkey", "idx_session_handle_id", "idx_session_flow_execution",
		"idx_session_absolute_expires_at", "SESSION_PARTICIPANT_pkey", "idx_session_participant_app",
	}
)

// legacySessionState is the source schema: the SSO tables exist and none of the new objects do.
func legacySessionState() sessionState {
	return sessionState{
		relations: map[string]string{
			legacySessionTable: relationKindTable, legacySessionParticipantTable: relationKindTable,
		},
		legacyPrimaryKeys: map[string]string{
			legacySessionTable:            sessionPrimaryKeyColumns,
			legacySessionParticipantTable: sessionParticipantPrimaryKeyColumns,
		},
	}
}

// renamedSessionState is the schema after operation 01: renamed tables, compatibility views, the TYPE
// column with its default, and nothing else of this release.
func renamedSessionState() sessionState {
	return sessionState{
		legacyPrimaryKeys: map[string]string{sessionParticipantTable: sessionParticipantPrimaryKeyColumns},
		participantColumns: []engine.QueryRow{{
			"column_name": "ou_id", "is_nullable": "NO", "data_type": "character varying",
			"maximum_length": "36", "column_default": "''::character varying",
		}},
		relations: map[string]string{
			sessionTable: relationKindTable, sessionParticipantTable: relationKindTable,
			legacySessionTable: relationKindView, legacySessionParticipantTable: relationKindView,
		},
		typeColumn: sessionTypeColumnRow("NO", "character varying", 10, "yes"),
		indexes:    sessionIndexRows(legacyIndexNames...),
		subjectColumns: []engine.QueryRow{
			{"column_name": "DEPLOYMENT_ID", "is_nullable": "NO"},
			{"column_name": "SUBJECT_ID", "is_nullable": "NO"},
		},
	}
}

func subjectIndexRow(unique bool, valid bool, ready bool, hasPredicate bool, method string, columns string) []engine.QueryRow {
	return []engine.QueryRow{{
		"is_unique": unique, "is_valid": valid, "is_ready": ready, "has_predicate": hasPredicate,
		"access_method": method, "column_names": columns,
	}}
}

func TestIsSessionTypeCheckDefinition(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		expected   bool
	}{
		{"postgres rendering", sessionTypeCheckDefinition, true},
		{"plain in list", `CHECK (type IN ('DIRECT', 'SSO'))`, true},
		{"missing direct", `CHECK (type IN ('SSO'))`, false},
		{"extra value", `CHECK (type IN ('SSO', 'DIRECT', 'OTHER'))`, false},
		{"other column", `CHECK (state IN ('SSO', 'DIRECT'))`, false},
		{"no literals", `CHECK (type IS NOT NULL)`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, isSessionTypeCheckDefinition(test.definition))
		})
	}
}

func TestSessionReadinessBlocksInvalidVerification(t *testing.T) {
	invalid := invalidSessionVerification("code", "broken")

	result := sessionReadiness(invalid, "fix it")

	assert.Equal(t, engine.ReadinessBlocked, result.State)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "code", result.Findings[0].Code)
	assert.Equal(t, "fix it", result.Findings[0].Remediation)
	assert.Equal(t, engine.ReadinessReady,
		sessionReadiness(sessionVerification(engine.VerificationSource, "ok"), "").State)
}

func TestSessionQueryIDsAreUnique(t *testing.T) {
	queries := []dbmodel.DBQuery{
		querySessionRelations, querySessionPrimaryKey, querySessionTypeColumn, querySessionIndexNames,
		querySessionCheckConstraints, querySessionSubjectColumns, querySessionSubjectIndex,
		querySessionCleanupReferences,
	}
	operation, err := NewAddSessionSubjectIndexOperation()
	require.NoError(t, err)
	indexOperation := operation.(*sessionSubjectIndexOperation)
	queries = append(queries, indexOperation.createIndexQuery, indexOperation.dropIndexQuery)

	seen := map[string]bool{}
	for _, query := range queries {
		assert.NotEmpty(t, query.PostgresQuery, query.ID)
		assert.False(t, seen[query.GetID()], "duplicate query id %s", query.GetID())
		seen[query.GetID()] = true
	}
}
