// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package v2_0_0

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
	"github.com/thunder-id/thunderid/internal/system/migration/engine"
)

const (
	mergeDuplicateLinkedAccountsOperationID engine.OperationID = "2.0.0/entitydb/" +
		"67_merge_duplicate_linked_accounts"
	mergeDuplicateLinkedAccountsRollbackID engine.OperationID = "2.0.0/entitydb/" +
		"67_merge_duplicate_linked_accounts_rollback"

	// mergeDuplicateLinkedAccountsBatchSize is the number of originals whose links are merged per batch.
	mergeDuplicateLinkedAccountsBatchSize = 100

	// mergedLinksCTE lists the distinct (original, IdP, subject) links of every person that has duplicates,
	// from the snapshot of each user's links in the working table. $1 limits it to the given originals, or to
	// every original when NULL, and $2 limits it to one deployment.
	mergedLinksCTE = `links AS (
			SELECT DISTINCT m.DEPLOYMENT_ID AS DEP, m.ORIGINAL_USER_ID AS OID, c.KEY AS IDP, s.KEY AS SUB
			FROM "USER_SHARE_MIGRATION" m
			CROSS JOIN LATERAL JSONB_EACH(m.LINKED_ACCOUNTS) c
			CROSS JOIN LATERAL JSONB_OBJECT_KEYS(c.VALUE) s(KEY)
			WHERE ($1::TEXT[] IS NULL OR m.ORIGINAL_USER_ID = ANY($1::TEXT[])) AND ($2 = '' OR m.DEPLOYMENT_ID = $2)
				AND EXISTS (SELECT 1 FROM "USER_SHARE_MIGRATION" d
					WHERE d.DEPLOYMENT_ID = m.DEPLOYMENT_ID AND d.ORIGINAL_USER_ID = m.ORIGINAL_USER_ID
						AND NOT d.IS_ORIGINAL)
		)`
)

var (
	// queryMergeLinkedAccounts replaces the original's linkedIds with the union of its own and its
	// duplicates' links, keyed by IdP and subject, so a subject held by several users is stored once. Other
	// system attributes are kept.
	queryMergeLinkedAccounts = dbmodel.DBQuery{
		ID: "MIG-USM-13-01",
		PostgresQuery: `WITH ` + mergedLinksCTE + `,
			per_idp AS (
				SELECT DEP, OID, IDP, JSONB_OBJECT_AGG(SUB, '{}'::JSONB) AS SUBS FROM links GROUP BY DEP, OID, IDP
			), merged AS (
				SELECT DEP, OID, JSONB_OBJECT_AGG(IDP, SUBS) AS LINKS FROM per_idp GROUP BY DEP, OID
			)
			UPDATE "ENTITY" e
			SET SYSTEM_ATTRIBUTES = JSONB_SET(COALESCE(e.SYSTEM_ATTRIBUTES, '{}'::JSONB), '{linkedIds}',
				merged.LINKS, TRUE)
			FROM merged
			WHERE e.ID = merged.OID AND e.DEPLOYMENT_ID = merged.DEP AND e.CATEGORY = 'user'
				AND (e.SYSTEM_ATTRIBUTES -> 'linkedIds') IS DISTINCT FROM merged.LINKS`,
	}
	// queryIndexLinkedAccounts adds the identifier row the entity store keeps for each linked subject. The
	// conflict target is omitted so that the statement works with any unique key on the table.
	queryIndexLinkedAccounts = dbmodel.DBQuery{
		ID: "MIG-USM-13-02",
		PostgresQuery: `WITH ` + mergedLinksCTE + `
			INSERT INTO "ENTITY_IDENTIFIER" (ENTITY_ID, NAME, VALUE, SOURCE, CREATED_AT, DEPLOYMENT_ID)
			SELECT OID, 'linkedIds.' || IDP, SUB, 'system', NOW(), DEP FROM links
			ON CONFLICT DO NOTHING`,
	}
	// queryLinkConflicts lists the originals linked, across their users, to more than one subject of one IdP.
	// A person has one subject per IdP, so users linked to different ones are not the same person.
	queryLinkConflicts = dbmodel.DBQuery{
		ID: "MIG-USM-13-03",
		PostgresQuery: `SELECT m.ORIGINAL_USER_ID AS ORIGINAL_USER_ID, c.KEY AS IDP
			FROM "USER_SHARE_MIGRATION" m
			CROSS JOIN LATERAL JSONB_EACH(m.LINKED_ACCOUNTS) c
			CROSS JOIN LATERAL JSONB_OBJECT_KEYS(c.VALUE) s(KEY)
			WHERE ($1 = '' OR m.DEPLOYMENT_ID = $1)
			GROUP BY m.DEPLOYMENT_ID, m.ORIGINAL_USER_ID, c.KEY
			HAVING COUNT(DISTINCT s.KEY) > 1
			ORDER BY 1, 2`,
	}
	// queryMissingLinks counts the merged links an original still lacks in its attributes or in the index.
	queryMissingLinks = dbmodel.DBQuery{
		ID: "MIG-USM-13-04",
		PostgresQuery: `WITH ` + mergedLinksCTE + `
			SELECT COUNT(*) AS links,
				COUNT(*) FILTER (WHERE NOT JSONB_EXISTS(COALESCE(e.SYSTEM_ATTRIBUTES -> 'linkedIds' -> l.IDP,
					'{}'::JSONB), l.SUB)) AS missing_in_attributes,
				COUNT(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM "ENTITY_IDENTIFIER" i
					WHERE i.ENTITY_ID = l.OID AND i.DEPLOYMENT_ID = l.DEP AND i.NAME = 'linkedIds.' || l.IDP
						AND i.VALUE = l.SUB)) AS missing_in_index
			FROM links l LEFT JOIN "ENTITY" e ON e.ID = l.OID AND e.DEPLOYMENT_ID = l.DEP AND e.CATEGORY = 'user'`,
	}
	// queryRestoreOriginalLinks puts back each original's own links, as the snapshot recorded them.
	queryRestoreOriginalLinks = dbmodel.DBQuery{
		ID: "MIG-USM-13-05",
		PostgresQuery: `UPDATE "ENTITY" e
			SET SYSTEM_ATTRIBUTES = CASE WHEN m.LINKED_ACCOUNTS = '{}'::JSONB
					THEN COALESCE(e.SYSTEM_ATTRIBUTES, '{}'::JSONB) - 'linkedIds'
					ELSE JSONB_SET(COALESCE(e.SYSTEM_ATTRIBUTES, '{}'::JSONB), '{linkedIds}', m.LINKED_ACCOUNTS, TRUE)
				END
			FROM "USER_SHARE_MIGRATION" m
			WHERE m.USER_ID = ANY($1::TEXT[]) AND m.IS_ORIGINAL AND ($2 = '' OR m.DEPLOYMENT_ID = $2)
				AND e.ID = m.USER_ID AND e.DEPLOYMENT_ID = m.DEPLOYMENT_ID AND e.CATEGORY = 'user'
				AND EXISTS (SELECT 1 FROM "USER_SHARE_MIGRATION" d
					WHERE d.DEPLOYMENT_ID = m.DEPLOYMENT_ID AND d.ORIGINAL_USER_ID = m.USER_ID AND NOT d.IS_ORIGINAL)
				AND (e.SYSTEM_ATTRIBUTES -> 'linkedIds')
					IS DISTINCT FROM NULLIF(m.LINKED_ACCOUNTS, '{}'::JSONB)`,
	}
	// queryRemoveMergedIdentifiers removes the identifier rows the merge added: the duplicates' links that the
	// original did not hold itself.
	queryRemoveMergedIdentifiers = dbmodel.DBQuery{
		ID: "MIG-USM-13-06",
		PostgresQuery: `DELETE FROM "ENTITY_IDENTIFIER" i
			USING (
				SELECT DISTINCT d.DEPLOYMENT_ID AS DEP, d.ORIGINAL_USER_ID AS OID, c.KEY AS IDP, s.KEY AS SUB
				FROM "USER_SHARE_MIGRATION" d
				CROSS JOIN LATERAL JSONB_EACH(d.LINKED_ACCOUNTS) c
				CROSS JOIN LATERAL JSONB_OBJECT_KEYS(c.VALUE) s(KEY)
				WHERE NOT d.IS_ORIGINAL AND d.ORIGINAL_USER_ID = ANY($1::TEXT[]) AND ($2 = '' OR d.DEPLOYMENT_ID = $2)
			) x
			WHERE i.ENTITY_ID = x.OID AND i.DEPLOYMENT_ID = x.DEP AND i.NAME = 'linkedIds.' || x.IDP
				AND i.VALUE = x.SUB
				AND NOT EXISTS (SELECT 1 FROM "USER_SHARE_MIGRATION" o
					WHERE o.USER_ID = x.OID AND o.DEPLOYMENT_ID = x.DEP
						AND JSONB_EXISTS(COALESCE(o.LINKED_ACCOUNTS -> x.IDP, '{}'::JSONB), x.SUB))`,
	}
	// queryMergeResidue counts, for the rollback, the originals whose links still differ from their own and the
	// merged identifier rows that remain.
	queryMergeResidue = dbmodel.DBQuery{
		ID: "MIG-USM-13-07",
		PostgresQuery: `SELECT
				(SELECT COUNT(*) FROM "USER_SHARE_MIGRATION" m
					INNER JOIN "ENTITY" e ON e.ID = m.USER_ID AND e.DEPLOYMENT_ID = m.DEPLOYMENT_ID
						AND e.CATEGORY = 'user'
					WHERE m.IS_ORIGINAL AND ($1 = '' OR m.DEPLOYMENT_ID = $1)
						AND EXISTS (SELECT 1 FROM "USER_SHARE_MIGRATION" d
							WHERE d.DEPLOYMENT_ID = m.DEPLOYMENT_ID AND d.ORIGINAL_USER_ID = m.USER_ID
								AND NOT d.IS_ORIGINAL)
						AND (e.SYSTEM_ATTRIBUTES -> 'linkedIds') IS DISTINCT FROM NULLIF(m.LINKED_ACCOUNTS, '{}'::JSONB)
				) AS wrong_links,
				(SELECT COUNT(*) FROM (
					SELECT DISTINCT d.DEPLOYMENT_ID AS DEP, d.ORIGINAL_USER_ID AS OID, c.KEY AS IDP, s.KEY AS SUB
					FROM "USER_SHARE_MIGRATION" d
					CROSS JOIN LATERAL JSONB_EACH(d.LINKED_ACCOUNTS) c
					CROSS JOIN LATERAL JSONB_OBJECT_KEYS(c.VALUE) s(KEY)
					WHERE NOT d.IS_ORIGINAL AND ($1 = '' OR d.DEPLOYMENT_ID = $1)
				) x INNER JOIN "ENTITY_IDENTIFIER" i ON i.ENTITY_ID = x.OID AND i.DEPLOYMENT_ID = x.DEP
					AND i.NAME = 'linkedIds.' || x.IDP AND i.VALUE = x.SUB
				WHERE NOT EXISTS (SELECT 1 FROM "USER_SHARE_MIGRATION" o
					WHERE o.USER_ID = x.OID AND o.DEPLOYMENT_ID = x.DEP
						AND JSONB_EXISTS(COALESCE(o.LINKED_ACCOUNTS -> x.IDP, '{}'::JSONB), x.SUB))
				) AS remaining_identifiers`,
	}
)

// newMergeDuplicateLinkedAccountsOperation gives each original every federated subject its duplicates were
// linked to, so a sign-in through any of those subjects resolves to the original once the duplicates are gone.
// The links are taken from the working table and written with set-based statements: SYSTEM_ATTRIBUTES.linkedIds
// on the original, and one ENTITY_IDENTIFIER row per linked subject, as the entity store does.
func newMergeDuplicateLinkedAccountsOperation() engine.Operation {
	return newBatchOperation(mergeDuplicateLinkedAccountsOperationID,
		"Link each original user to the federated subjects of its duplicates",
		engine.StoreIDEntityDB, nil,
		readinessAfterMigrationTable(snapshotDuplicateUsersOperationID, verifyDuplicateLinksMerged),
		verifyDuplicateLinksMerged, mergeDuplicateLinkedAccountsBatch)
}

func newRollbackMergeDuplicateLinkedAccountsOperation() engine.RollbackOperation {
	return &rollbackBatchOperation{
		batchOperation: newBatchOperation(mergeDuplicateLinkedAccountsRollbackID,
			"Restore each original user's own federated subject links",
			engine.StoreIDEntityDB, nil, nil, verifyOriginalLinksRestored, restoreOriginalLinksBatch),
		forwardID: mergeDuplicateLinkedAccountsOperationID,
	}
}

func mergeDuplicateLinkedAccountsBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatchWithIDs(ctx, access, target, checkpoint, mergeDuplicateLinkedAccountsBatchSize,
		func(originalIDs []string) error {
			conflicts, err := readLinkConflicts(ctx, access, target.DeploymentID)
			if err != nil {
				return err
			}
			if conflicts != "" {
				return errors.New(conflicts)
			}
			ids := textArray(originalIDs)
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryMergeLinkedAccounts,
				ids, target.DeploymentID); err != nil {
				return fmt.Errorf("merge the linked accounts of duplicate users: %w", err)
			}
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryIndexLinkedAccounts,
				ids, target.DeploymentID); err != nil {
				return fmt.Errorf("index the merged linked accounts: %w", err)
			}
			return nil
		})
}

func restoreOriginalLinksBatch(
	ctx context.Context,
	access engine.ExecutionAccess,
	target engine.ExecutionTarget,
	checkpoint batchCheckpoint,
) (batchCheckpoint, bool, error) {
	return groupBatchWithIDs(ctx, access, target, checkpoint, mergeDuplicateLinkedAccountsBatchSize,
		func(originalIDs []string) error {
			ids := textArray(originalIDs)
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryRestoreOriginalLinks,
				ids, target.DeploymentID); err != nil {
				return fmt.Errorf("restore the linked accounts of original users: %w", err)
			}
			if _, err := access.Execute(ctx, engine.StoreIDEntityDB, queryRemoveMergedIdentifiers,
				ids, target.DeploymentID); err != nil {
				return fmt.Errorf("remove the merged linked account identifiers: %w", err)
			}
			return nil
		})
}

// readLinkConflicts describes the originals whose users are linked to different subjects of one IdP, or
// returns an empty string.
func readLinkConflicts(ctx context.Context, access engine.ReadAccess, deploymentID string) (string, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryLinkConflicts, deploymentID)
	if err != nil {
		return "", fmt.Errorf("check linked accounts: %w", err)
	}
	findings := make([]string, 0, len(rows))
	for _, row := range rows {
		originalID, originalErr := row.String("original_user_id")
		idp, idpErr := row.String("idp")
		if err := errors.Join(originalErr, idpErr); err != nil {
			return "", fmt.Errorf("read linked account conflict: %w", err)
		}
		findings = append(findings, fmt.Sprintf(
			"User %q and its duplicates are linked to different subjects of connection %q", originalID, idp))
	}
	return strings.Join(findings, "; "), nil
}

// verifyDuplicateLinksMerged is at target when every original that has duplicates holds, and indexes, the
// links of all its users. Links are keyed by connection and subject, so a subject held by several users is
// stored once. It is invalid when users of one email are linked to different subjects of one connection.
func verifyDuplicateLinksMerged(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error) {
	conflicts, err := readLinkConflicts(ctx, access, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if conflicts != "" {
		return invalidVerification("conflicting_linked_accounts", conflicts), nil
	}
	links, inAttributes, inIndex, err := countMissingLinks(ctx, access, nil, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, err
	}
	if inAttributes == 0 && inIndex == 0 {
		return engine.VerificationResult{State: engine.VerificationTarget, Summary: fmt.Sprintf(
			"All %d merged links are held and indexed by their original users", links)}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d merged links are missing in attributes and %d in the index", inAttributes, inIndex)}, nil
}

// countMissingLinks counts the merged links of the given originals, or of every original when ids is nil.
func countMissingLinks(ctx context.Context, access engine.ReadAccess, originalIDs []string, deploymentID string) (
	int64, int64, int64, error) {
	var ids interface{}
	if originalIDs != nil {
		ids = textArray(originalIDs)
	}
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryMissingLinks, ids, deploymentID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count merged links: %w", err)
	}
	if len(rows) != 1 {
		return 0, 0, 0, fmt.Errorf("count merged links: expected one row, found %d", len(rows))
	}
	links, linksErr := rows[0].Int64("links")
	inAttributes, attributesErr := rows[0].Int64("missing_in_attributes")
	inIndex, indexErr := rows[0].Int64("missing_in_index")
	if err := errors.Join(linksErr, attributesErr, indexErr); err != nil {
		return 0, 0, 0, err
	}
	return links, inAttributes, inIndex, nil
}

// verifyOriginalLinksRestored is at target when every original holds exactly its own links again and no
// identifier added by the merge remains.
func verifyOriginalLinksRestored(ctx context.Context, access engine.ReadAccess, deploymentID string) (
	engine.VerificationResult, error) {
	rows, err := access.Query(ctx, engine.StoreIDEntityDB, queryMergeResidue, deploymentID)
	if err != nil {
		return engine.VerificationResult{}, fmt.Errorf("count merged links left by the merge: %w", err)
	}
	if len(rows) != 1 {
		return engine.VerificationResult{}, fmt.Errorf("count merged links left by the merge: expected one row")
	}
	wrongLinks, wrongErr := rows[0].Int64("wrong_links")
	remaining, remainingErr := rows[0].Int64("remaining_identifiers")
	if err := errors.Join(wrongErr, remainingErr); err != nil {
		return engine.VerificationResult{}, err
	}
	if wrongLinks == 0 && remaining == 0 {
		return engine.VerificationResult{State: engine.VerificationTarget,
			Summary: "All original users hold only their own links"}, nil
	}
	return engine.VerificationResult{State: engine.VerificationSource, Summary: fmt.Sprintf(
		"%d original users still hold merged links and %d merged identifiers remain", wrongLinks, remaining)}, nil
}
