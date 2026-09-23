package tagging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/service/tagging"
)

const adminItemQuery = `
SELECT tag.id,tag.name,tag.status,tag.version,tag.created_at_ms,tag.updated_at_ms,tag.deleted_at_ms,
  (SELECT count(*) FROM game_tags relation JOIN games game ON game.id=relation.game_id
   WHERE relation.tag_id=tag.id AND game.status='PUBLISHED'),
  (SELECT count(*) FROM game_tags relation JOIN games game ON game.id=relation.game_id
   WHERE relation.tag_id=tag.id AND game.status='DELETED'),
  (SELECT count(*) FROM review_draft_tags relation
   JOIN import_items draft ON draft.id=relation.review_draft_id
   JOIN import_items item ON item.id=draft.id
   WHERE relation.tag_id=tag.id AND (tag.status='DELETED' OR item.state='REVIEW_PENDING')),
  (SELECT count(*) FROM source_collection_tags relation WHERE relation.tag_id=tag.id)
FROM tags tag`

func scanAdminItem(row dbexec.Scanner) (tagging.AdminItem, error) {
	var result tagging.AdminItem
	var deletedAt sql.NullInt64
	err := row.Scan(
		&result.TagID, &result.Name, &result.Status, &result.Version,
		&result.CreatedAtMS, &result.UpdatedAtMS, &deletedAt,
		&result.Usage.PublishedGameCount, &result.Usage.DeletedGameCount,
		&result.Usage.ReviewDraftCount, &result.Usage.SourceCollectionCount,
	)
	if deletedAt.Valid {
		result.DeletedAtMS = &deletedAt.Int64
	}
	if err != nil {
		return result, fmt.Errorf("tagging: scan admin item: %w", err)
	}
	return result, nil
}

func (records tagRecords) Get(ctx context.Context, tagID string) (tagging.AdminItem, error) {
	result, err := scanAdminItem(records.database.QueryRowContext(ctx, adminItemQuery+` WHERE tag.id=?`, tagID))
	if errors.Is(err, sql.ErrNoRows) {
		return tagging.AdminItem{}, tagging.ErrNotFound
	}
	if err != nil {
		return tagging.AdminItem{}, fmt.Errorf("tagging: read tag: %w", err)
	}
	return result, nil
}

func (repository *Repository) Get(ctx context.Context, tagID string) (tagging.AdminItem, error) {
	return (tagRecords{repository.database}).Get(ctx, tagID)
}

func (repository *Repository) List(ctx context.Context, filter tagging.ListQuery) ([]tagging.AdminItem, error) {
	conditions := []string{"1=1"}
	arguments := []any{}
	if filter.Status != "ALL" {
		conditions = append(conditions, "tag.status=?")
		arguments = append(arguments, filter.Status)
	}
	if filter.SearchText != "" {
		conditions = append(conditions, "instr(tag.search_text,?)>0")
		arguments = append(arguments, filter.SearchText)
	}
	order := "tag.name_key,tag.id"
	if filter.Sort == tagging.SortUpdatedDesc {
		order = "tag.updated_at_ms DESC,tag.id DESC"
	}
	if filter.AfterID != "" {
		if filter.Sort == tagging.SortNameAsc {
			conditions = append(conditions, "(tag.name_key>? OR (tag.name_key=? AND tag.id>?))")
			arguments = append(arguments, filter.AfterNameKey, filter.AfterNameKey, filter.AfterID)
		} else {
			conditions = append(conditions, "(tag.updated_at_ms<? OR (tag.updated_at_ms=? AND tag.id<?))")
			arguments = append(arguments, filter.AfterUpdatedAt, filter.AfterUpdatedAt, filter.AfterID)
		}
	}
	arguments = append(arguments, filter.Limit)
	rows, err := repository.database.QueryContext(
		ctx,
		adminItemQuery+` WHERE `+strings.Join(
			conditions,
			" AND ",
		)+` ORDER BY `+order+` LIMIT ?`,
		arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("tagging: list tags: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]tagging.AdminItem, 0)
	for rows.Next() {
		item, err := scanAdminItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate tags: %w", err)
	}
	return result, nil
}

func (repository *Repository) Summary(ctx context.Context) (tagging.Summary, error) {
	var result tagging.Summary
	err := repository.database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM tags WHERE status='ACTIVE'),
  (SELECT count(DISTINCT relation.game_id) FROM game_tags relation
   JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'),
  (SELECT count(DISTINCT relation.review_draft_id) FROM review_draft_tags relation
   JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
   JOIN import_items draft ON draft.id=relation.review_draft_id
   JOIN import_items item ON item.id=draft.id AND item.state='REVIEW_PENDING')
`).Scan(&result.ActiveTagCount, &result.TaggedGameCount, &result.PendingReviewCount)
	if err != nil {
		return tagging.Summary{}, fmt.Errorf("tagging: summarize tags: %w", err)
	}
	return result, nil
}

func (records tagRecords) ActiveByNameKey(ctx context.Context) (map[string]string, error) {
	rows, err := records.database.QueryContext(ctx, `SELECT id,name_key FROM tags WHERE status='ACTIVE'`)
	if err != nil {
		return nil, fmt.Errorf("tagging: list active tag keys: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make(map[string]string)
	for rows.Next() {
		var id, nameKey string
		if err := rows.Scan(&id, &nameKey); err != nil {
			return nil, fmt.Errorf("tagging: scan active tag key: %w", err)
		}
		result[nameKey] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate active tag keys: %w", err)
	}
	return result, nil
}

func (records tagRecords) ActiveReferences(ctx context.Context, tagIDs []string) ([]tagging.Reference, error) {
	rows, err := records.database.QueryContext(ctx, `
SELECT id,name FROM tags
WHERE status='ACTIVE' AND id IN (SELECT value FROM json_each(?))
ORDER BY name_key,id
`, encodedIDs(tagIDs))
	if err != nil {
		return nil, fmt.Errorf("tagging: validate references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]tagging.Reference, 0, len(tagIDs))
	for rows.Next() {
		var reference tagging.Reference
		if err := rows.Scan(&reference.TagID, &reference.Name); err != nil {
			return nil, fmt.Errorf("tagging: scan reference: %w", err)
		}
		result = append(result, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate references: %w", err)
	}
	return result, nil
}
