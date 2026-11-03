// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	testGoogleConnectionID = "google-connection"
	testGitHubConnectionID = "github-connection"
	testGoogleSubject      = "117234981234567890123"
	testGitHubSubject      = "12345678"
	testDeployment         = "deployment-1"
)

type staticDeclarativeConnections struct {
	connections []DeclarativeConnection
}

func (s staticDeclarativeConnections) ListDeclarativeConnections(
	context.Context,
) ([]DeclarativeConnection, error) {
	return s.connections, nil
}

func testConnectionSource(connections ...DeclarativeConnection) *declarativeConnectionSource {
	return newDeclarativeConnectionSource(func() (DeclarativeConnections, error) {
		return staticDeclarativeConnections{connections: connections}, nil
	})
}

func googleAndGitHubConnections() *declarativeConnectionSource {
	return testConnectionSource(
		DeclarativeConnection{ID: testGoogleConnectionID, Type: "google"},
		DeclarativeConnection{ID: testGitHubConnectionID, Type: "github"},
		DeclarativeConnection{ID: "other-connection", Type: "oidc"},
	)
}

func legacyUserRow(id, attributes, systemAttributes string) engine.QueryRow {
	return engine.QueryRow{
		"id": id, "type": "customer", "deployment_id": testDeployment,
		"attributes": attributes, "system_attributes": systemAttributes,
	}
}

func linkedIdentifierRow(entityID, idpID, subject, source string) engine.QueryRow {
	return engine.QueryRow{
		"entity_id": entityID, "deployment_id": testDeployment,
		"name": linkedIdentifierPrefix + idpID, "value": subject, "source": source,
	}
}

func legacySubjectAccess(users []engine.QueryRow, identifiers []engine.QueryRow) agentNameReadAccess {
	return agentNameReadAccess{rows: map[string][]engine.QueryRow{
		queryLegacySubjectUsers.GetID():       users,
		queryLinkedSubjectIdentifiers.GetID(): identifiers,
	}}
}

func linkedSystemAttributes(idpID, subject string) string {
	return `{"linkedIds":{"` + idpID + `":{"` + subject + `":{}}}}`
}

func TestLegacySubjectLinkPersistence(t *testing.T) {
	link := legacySubjectLink{idpID: testGoogleConnectionID, subject: testGoogleSubject}

	t.Run("adds a link to empty system attributes", func(t *testing.T) {
		systemAttributes := map[string]interface{}{}
		changed, err := addLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, map[string]interface{}{
			"linkedIds": map[string]interface{}{
				testGoogleConnectionID: map[string]interface{}{testGoogleSubject: map[string]interface{}{}},
			},
		}, systemAttributes)
	})

	t.Run("is idempotent", func(t *testing.T) {
		systemAttributes := map[string]interface{}{}
		_, err := addLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		changed, err := addLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("keeps unrelated system attributes and other links", func(t *testing.T) {
		systemAttributes := map[string]interface{}{
			"display": "name",
			"linkedIds": map[string]interface{}{
				"other": map[string]interface{}{"x": map[string]interface{}{}},
			},
		}
		changed, err := addLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "name", systemAttributes["display"])
		links := systemAttributes["linkedIds"].(map[string]interface{})
		assert.Contains(t, links, "other")
		assert.Contains(t, links, testGoogleConnectionID)
	})

	t.Run("rejects a linked identities value that is not an object", func(t *testing.T) {
		for name, systemAttributes := range map[string]map[string]interface{}{
			"linkedIds is a string": {"linkedIds": "x"},
			"linkedIds is a number": {"linkedIds": float64(1)},
			"connection is an array": {
				"linkedIds": map[string]interface{}{testGoogleConnectionID: []interface{}{}},
			},
		} {
			t.Run(name, func(t *testing.T) {
				_, err := addLegacySubjectLink(systemAttributes, link)
				require.Error(t, err)
			})
		}
	})

	t.Run("treats a null connection entry as absent", func(t *testing.T) {
		systemAttributes := map[string]interface{}{
			"linkedIds": map[string]interface{}{testGoogleConnectionID: nil},
		}
		changed, err := addLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.True(t, changed)
	})

	t.Run("removal restores the source representation", func(t *testing.T) {
		systemAttributes := map[string]interface{}{"display": "name"}
		_, err := addLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		changed, err := removeLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, map[string]interface{}{"display": "name"}, systemAttributes)
	})

	t.Run("removal keeps other subjects of the connection", func(t *testing.T) {
		systemAttributes := map[string]interface{}{}
		other := legacySubjectLink{idpID: testGoogleConnectionID, subject: "other-subject"}
		_, _ = addLegacySubjectLink(systemAttributes, link)
		_, _ = addLegacySubjectLink(systemAttributes, other)
		changed, err := removeLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.True(t, changed)
		has, _ := hasLegacySubjectLink(systemAttributes, other)
		assert.True(t, has)
	})

	t.Run("removal of a missing link changes nothing", func(t *testing.T) {
		systemAttributes := map[string]interface{}{"display": "name"}
		changed, err := removeLegacySubjectLink(systemAttributes, link)
		require.NoError(t, err)
		assert.False(t, changed)
	})
}

func TestLegacySubjectLinkFor(t *testing.T) {
	ids := map[legacySubjectProvider][]string{
		legacySubjectProviderGoogle: {testGoogleConnectionID},
		legacySubjectProviderGitHub: {testGitHubConnectionID},
	}
	record := func(sub interface{}) legacySubjectRecord {
		return legacySubjectRecord{id: "user-1", attributes: map[string]interface{}{"sub": sub}}
	}

	t.Run("maps a Google subject to the Google connection", func(t *testing.T) {
		link, err := legacySubjectLinkFor(record(testGoogleSubject), ids)
		require.NoError(t, err)
		assert.Equal(t, legacySubjectLink{idpID: testGoogleConnectionID, subject: testGoogleSubject}, link)
		assert.Equal(t, "linkedIds."+testGoogleConnectionID, link.identifierName())
	})

	t.Run("maps a GitHub subject to the GitHub connection", func(t *testing.T) {
		link, err := legacySubjectLinkFor(record(testGitHubSubject), ids)
		require.NoError(t, err)
		assert.Equal(t, testGitHubConnectionID, link.idpID)
	})

	t.Run("rejects a subject that is not a string", func(t *testing.T) {
		_, err := legacySubjectLinkFor(record(float64(12345678)), ids)
		require.Error(t, err)
	})

	t.Run("rejects a subject that fits no provider", func(t *testing.T) {
		_, err := legacySubjectLinkFor(record("abc"), ids)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "abc")
	})

	t.Run("rejects a provider without a connection", func(t *testing.T) {
		_, err := legacySubjectLinkFor(record(testGitHubSubject),
			map[legacySubjectProvider][]string{legacySubjectProviderGoogle: {testGoogleConnectionID}})
		require.Error(t, err)
	})

	t.Run("rejects a provider with several connections", func(t *testing.T) {
		_, err := legacySubjectLinkFor(record(testGoogleSubject),
			map[legacySubjectProvider][]string{legacySubjectProviderGoogle: {"a", "b"}})
		require.Error(t, err)
	})
}

func TestLegacySubjectVerification(t *testing.T) {
	forward := newLinkLegacyFederatedSubjectsOperation(googleAndGitHubConnections())
	rollback := newRollbackLinkLegacyFederatedSubjectsOperation(googleAndGitHubConnections())
	target := engine.ExecutionTarget{}

	needsLink := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`, `{}`)
	linked := legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`,
		linkedSystemAttributes(testGoogleConnectionID, testGoogleSubject))
	linkedIdentifier := linkedIdentifierRow("user-1", testGoogleConnectionID, testGoogleSubject, "system")

	tests := []struct {
		name        string
		operation   engine.Operation
		users       []engine.QueryRow
		identifiers []engine.QueryRow
		want        engine.VerificationState
	}{
		{name: "no user holds a subject is the target", operation: forward, want: engine.VerificationTarget},
		{name: "a user without a link is the source", operation: forward,
			users: []engine.QueryRow{needsLink}, want: engine.VerificationSource},
		{name: "a user with a link and an identifier is the target", operation: forward,
			users: []engine.QueryRow{linked}, identifiers: []engine.QueryRow{linkedIdentifier},
			want: engine.VerificationTarget},
		{name: "a link without an identifier row is the source", operation: forward,
			users: []engine.QueryRow{linked}, want: engine.VerificationSource},
		{name: "an identifier row with another source is the source", operation: forward,
			users: []engine.QueryRow{linked},
			identifiers: []engine.QueryRow{
				linkedIdentifierRow("user-1", testGoogleConnectionID, testGoogleSubject, "attribute"),
			},
			want: engine.VerificationSource},
		{name: "a subject that fits no provider is invalid", operation: forward,
			users: []engine.QueryRow{legacyUserRow("user-1", `{"sub":"abc"}`, `{}`)},
			want:  engine.VerificationInvalid},
		{name: "a subject stored as a number is invalid", operation: forward,
			users: []engine.QueryRow{legacyUserRow("user-1", `{"sub":12345678}`, `{}`)},
			want:  engine.VerificationInvalid},
		{name: "an unreadable link representation is invalid", operation: forward,
			users: []engine.QueryRow{legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`,
				`{"linkedIds":"x"}`)},
			want: engine.VerificationInvalid},
		{name: "rollback of a linked user is the source", operation: rollback,
			users: []engine.QueryRow{linked}, identifiers: []engine.QueryRow{linkedIdentifier},
			want: engine.VerificationSource},
		{name: "rollback of an unlinked user is the target", operation: rollback,
			users: []engine.QueryRow{needsLink}, want: engine.VerificationTarget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.operation.PreVerify(context.Background(),
				legacySubjectAccess(tt.users, tt.identifiers), target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.State, result.Summary)
		})
	}
}

func TestLegacySubjectVerificationWithoutConnections(t *testing.T) {
	users := []engine.QueryRow{legacyUserRow("user-1", `{"sub":"`+testGoogleSubject+`"}`, `{}`)}

	t.Run("a missing Google connection is invalid", func(t *testing.T) {
		operation := newLinkLegacyFederatedSubjectsOperation(
			testConnectionSource(DeclarativeConnection{ID: testGitHubConnectionID, Type: "github"}))
		result, err := operation.PreVerify(context.Background(), legacySubjectAccess(users, nil),
			engine.ExecutionTarget{})
		require.NoError(t, err)
		assert.Equal(t, engine.VerificationInvalid, result.State)
	})

	t.Run("no connection source is invalid when a user needs a link", func(t *testing.T) {
		operation := newLinkLegacyFederatedSubjectsOperation(nil)
		result, err := operation.PreVerify(context.Background(), legacySubjectAccess(users, nil),
			engine.ExecutionTarget{})
		require.NoError(t, err)
		assert.Equal(t, engine.VerificationInvalid, result.State)
	})

	t.Run("no connection source is fine when no user needs a link", func(t *testing.T) {
		operation := newLinkLegacyFederatedSubjectsOperation(nil)
		result, err := operation.PreVerify(context.Background(), legacySubjectAccess(nil, nil),
			engine.ExecutionTarget{})
		require.NoError(t, err)
		assert.Equal(t, engine.VerificationTarget, result.State)
	})
}

func TestLegacySubjectReadiness(t *testing.T) {
	operation := newLinkLegacyFederatedSubjectsOperation(googleAndGitHubConnections())
	checker, ok := operation.(engine.ReadinessChecker)
	require.True(t, ok)

	t.Run("is ready when every subject can be linked", func(t *testing.T) {
		result, err := checker.CheckReadiness(context.Background(), legacySubjectAccess(
			[]engine.QueryRow{legacyUserRow("user-1", `{"sub":"`+testGitHubSubject+`"}`, `{}`)}, nil),
			engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessReady, result.State)
	})

	t.Run("is blocked, with a finding, for a subject that cannot be classified", func(t *testing.T) {
		result, err := checker.CheckReadiness(context.Background(), legacySubjectAccess(
			[]engine.QueryRow{legacyUserRow("user-1", `{"sub":"abc"}`, `{}`)}, nil), engine.PlanContext{})
		require.NoError(t, err)
		assert.Equal(t, engine.ReadinessBlocked, result.State)
		require.Len(t, result.Findings, 1)
		assert.NotContains(t, result.Findings[0].Message, "abc")
	})
}

func TestLegacySubjectOperationDefinitions(t *testing.T) {
	connections := googleAndGitHubConnections()
	forward := newLinkLegacyFederatedSubjectsOperation(connections)
	catchUp := newLinkLegacyFederatedSubjectsCatchUpOperation(connections)
	rollback := newRollbackLinkLegacyFederatedSubjectsOperation(connections)

	assert.Equal(t, linkLegacyFederatedSubjectsOperationID, forward.Definition().ID)
	assert.Equal(t, linkLegacyFederatedSubjectsCatchUpOperationID, catchUp.Definition().ID)
	assert.Equal(t, linkLegacyFederatedSubjectsRollbackOperationID, rollback.Definition().ID)
	assert.Equal(t, linkLegacyFederatedSubjectsOperationID, rollback.ForwardOperationID())
	for _, operation := range []engine.Operation{forward, catchUp, rollback} {
		definition := operation.Definition()
		assert.Equal(t, engine.StoreIDEntityDB, definition.StoreID)
		assert.Equal(t, engine.OperationKindDataChange, definition.Kind)
		assert.Equal(t, engine.InterruptionModeResumable, definition.InterruptionMode)
		assert.Implements(t, (*engine.ResumableOperation)(nil), operation)
	}
}

func TestDeclarativeConnectionSourceGroupsIdentifiersByProvider(t *testing.T) {
	ids, err := googleAndGitHubConnections().idsByProvider(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{testGoogleConnectionID}, ids[legacySubjectProviderGoogle])
	assert.Equal(t, []string{testGitHubConnectionID}, ids[legacySubjectProviderGitHub])

	ids, err = testConnectionSource(DeclarativeConnection{ID: "g", Type: " Google "}).
		idsByProvider(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"g"}, ids[legacySubjectProviderGoogle])

	ids, err = (*declarativeConnectionSource)(nil).idsByProvider(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ids)
}
