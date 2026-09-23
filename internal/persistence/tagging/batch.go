package tagging

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/tagging"
)

func batchReferences(
	ctx context.Context,
	database *sql.DB,
	query string,
	ownerIDs []string,
) (map[string][]tagging.Reference, error) {
	result := make(map[string][]tagging.Reference, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return result, nil
	}
	rows, err := database.QueryContext(ctx, query, encodedIDs(ownerIDs))
	if err != nil {
		return nil, fmt.Errorf("tagging: query owner references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var ownerID string
		var reference tagging.Reference
		if err := rows.Scan(&ownerID, &reference.TagID, &reference.Name); err != nil {
			return nil, fmt.Errorf("tagging: scan owner references: %w", err)
		}
		result[ownerID] = append(result[ownerID], reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate owner references: %w", err)
	}
	return result, nil
}

func (repository *Repository) References(
	ctx context.Context,
	kind tagging.OwnerKind,
	ownerIDs []string,
) (map[string][]tagging.Reference, error) {
	if kind == tagging.OwnerReviewItem {
		return batchReferences(ctx, repository.database, `
SELECT draft.id,tag.id,tag.name FROM review_draft_tags relation
JOIN import_items draft ON draft.id=relation.review_draft_id
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE draft.id IN (SELECT value FROM json_each(?)) ORDER BY draft.id,tag.name_key,tag.id
`, ownerIDs)
	}
	table, column, err := ownerTable(kind)
	if err != nil {
		return nil, err
	}
	query := `SELECT relation.` + column + `,tag.id,tag.name FROM ` + table + ` relation
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.` + column + ` IN (SELECT value FROM json_each(?)) ORDER BY relation.` + column + `,tag.name_key,tag.id`
	return batchReferences(ctx, repository.database, query, ownerIDs)
}
