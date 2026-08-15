package tagging

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Service struct {
	database *sql.DB
	now      func() time.Time
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func (service *Service) withImmediateWrite(ctx context.Context, work func(*sql.Conn) error) error {
	connection, err := service.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("tagging: acquire connection: %w", err)
	}
	defer func() { cleanup.Error("close", connection.Close()) }()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("tagging: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if err := work(connection); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("tagging: commit: %w", err)
	}
	committed = true
	return nil
}

const adminItemQuery = `
SELECT tag.id,tag.name,tag.status,tag.version,tag.created_at_ms,tag.updated_at_ms,tag.deleted_at_ms,
  (SELECT count(*) FROM game_tags relation JOIN games game ON game.id=relation.game_id
   WHERE relation.tag_id=tag.id AND game.status='PUBLISHED'),
  (SELECT count(*) FROM game_tags relation JOIN games game ON game.id=relation.game_id
   WHERE relation.tag_id=tag.id AND game.status='DELETED'),
  (SELECT count(*) FROM review_draft_tags relation
   JOIN review_drafts draft ON draft.id=relation.review_draft_id
   JOIN import_items item ON item.id=draft.import_item_id
   WHERE relation.tag_id=tag.id AND (tag.status='DELETED' OR item.state='REVIEW_PENDING')),
  (SELECT count(*) FROM pegasus_collection_tags relation WHERE relation.tag_id=tag.id)
FROM tags tag`

type scanner interface{ Scan(...any) error }

func scanAdminItem(row scanner) (AdminItem, error) {
	var result AdminItem
	var deletedAt sql.NullInt64
	err := row.Scan(
		&result.TagID, &result.Name, &result.Status, &result.Version,
		&result.CreatedAtMS, &result.UpdatedAtMS, &deletedAt,
		&result.Usage.PublishedGameCount, &result.Usage.DeletedGameCount,
		&result.Usage.ReviewDraftCount, &result.Usage.PegasusCollectionCount,
	)
	if deletedAt.Valid {
		result.DeletedAtMS = &deletedAt.Int64
	}
	if err != nil {
		return result, fmt.Errorf("tagging: scan admin item: %w", err)
	}
	return result, nil
}

func adminItemByID(ctx context.Context, database executor, tagID string) (AdminItem, error) {
	result, err := scanAdminItem(database.QueryRowContext(ctx, adminItemQuery+` WHERE tag.id=?`, tagID))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminItem{}, ErrNotFound
	}
	if err != nil {
		return AdminItem{}, fmt.Errorf("tagging: read tag: %w", err)
	}
	return result, nil
}

func (service *Service) Get(ctx context.Context, tagID string) (AdminItem, error) {
	if !ValidID(tagID) {
		return AdminItem{}, ErrNotFound
	}
	return adminItemByID(ctx, service.database, tagID)
}

func validListFilter(filter ListFilter) bool {
	if filter.Status != StatusActive && filter.Status != StatusDeleted && filter.Status != "ALL" {
		return false
	}
	if filter.Sort != SortNameAsc && filter.Sort != SortUpdatedDesc {
		return false
	}
	return filter.Limit >= 1 && filter.Limit <= MaximumListLimit+1
}

func applyListCursor(filter ListFilter, conditions *[]string, arguments *[]any) error {
	if filter.AfterID == "" {
		return nil
	}
	if !ValidID(filter.AfterID) || len(filter.AfterValues) != 1 {
		return ErrInvalid
	}
	if filter.Sort == SortNameAsc {
		*conditions = append(*conditions, "(tag.name_key>? OR (tag.name_key=? AND tag.id>?))")
		*arguments = append(*arguments, filter.AfterValues[0], filter.AfterValues[0], filter.AfterID)
		return nil
	}
	updatedAt, err := strconv.ParseInt(filter.AfterValues[0], 10, 64)
	if err != nil || updatedAt < 0 {
		return ErrInvalid
	}
	*conditions = append(*conditions, "(tag.updated_at_ms<? OR (tag.updated_at_ms=? AND tag.id<?))")
	*arguments = append(*arguments, updatedAt, updatedAt, filter.AfterID)
	return nil
}

func (service *Service) List(ctx context.Context, filter ListFilter) ([]AdminItem, error) {
	if !validListFilter(filter) {
		return nil, ErrInvalid
	}
	conditions := []string{"1=1"}
	arguments := []any{}
	if filter.Status != "ALL" {
		conditions = append(conditions, "tag.status=?")
		arguments = append(arguments, filter.Status)
	}
	if filter.Query != "" {
		conditions = append(conditions, "instr(tag.search_text,?)>0")
		arguments = append(arguments, canonicalSearch(filter.Query))
	}
	order := "tag.name_key,tag.id"
	if err := applyListCursor(filter, &conditions, &arguments); err != nil {
		return nil, err
	}
	if filter.Sort == SortUpdatedDesc {
		order = "tag.updated_at_ms DESC,tag.id DESC"
	}
	arguments = append(arguments, filter.Limit)
	rows, err := service.database.QueryContext(ctx, adminItemQuery+` WHERE `+
		strings.Join(conditions, " AND ")+` ORDER BY `+order+` LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("tagging: list tags: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]AdminItem, 0)
	for rows.Next() {
		item, err := scanAdminItem(rows)
		if err != nil {
			return nil, fmt.Errorf("tagging: scan tag: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate tags: %w", err)
	}
	return result, nil
}

func (service *Service) Summary(ctx context.Context) (Summary, error) {
	var result Summary
	err := service.database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM tags WHERE status='ACTIVE'),
  (SELECT count(DISTINCT relation.game_id) FROM game_tags relation
   JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'),
  (SELECT count(DISTINCT relation.review_draft_id) FROM review_draft_tags relation
   JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
   JOIN review_drafts draft ON draft.id=relation.review_draft_id
   JOIN import_items item ON item.id=draft.import_item_id AND item.state='REVIEW_PENDING')
`).Scan(&result.ActiveTagCount, &result.TaggedGameCount, &result.PendingReviewCount)
	if err != nil {
		return Summary{}, fmt.Errorf("tagging: summarize tags: %w", err)
	}
	return result, nil
}

func auditJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func writeAudit(
	ctx context.Context,
	database executor,
	actorUserID, action, resourceType, resourceID string,
	before, after, diff any,
	now int64,
) error {
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("tagging: create audit id: %w", err)
	}
	var beforeJSON, afterJSON, diffJSON any
	if before != nil {
		beforeJSON = auditJSON(before)
	}
	if after != nil {
		afterJSON = auditJSON(after)
	}
	if diff != nil {
		diffJSON = auditJSON(diff)
	}
	_, err = database.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,?,?,?,?,?,?,NULL,?)
`, auditID.String(), actorUserID, action, resourceType, resourceID, beforeJSON, afterJSON, diffJSON, now)
	if err != nil {
		return fmt.Errorf("tagging: write audit: %w", err)
	}
	return nil
}

func (service *Service) Create(ctx context.Context, actorUserID, rawName string) (AdminItem, error) {
	if !ValidID(actorUserID) {
		return AdminItem{}, ErrInvalid
	}
	name, nameKey, searchText, err := NormalizeName(rawName)
	if err != nil {
		return AdminItem{}, err
	}
	var result AdminItem
	err = service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		var activeCount, conflict int
		if err := connection.QueryRowContext(
			ctx, `SELECT count(*) FROM tags WHERE status='ACTIVE'`,
		).Scan(&activeCount); err != nil {
			return fmt.Errorf("tagging: count active tags: %w", err)
		}
		if activeCount >= MaxActiveTags {
			return ErrLimitReached
		}
		if err := connection.QueryRowContext(
			ctx, `SELECT count(*) FROM tags WHERE status='ACTIVE' AND name_key=?`, nameKey,
		).Scan(&conflict); err != nil {
			return fmt.Errorf("tagging: check name: %w", err)
		}
		if conflict != 0 {
			return ErrNameConflict
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("tagging: create tag id: %w", err)
		}
		now := service.now().UnixMilli()
		if _, err := connection.ExecContext(ctx, `
INSERT INTO tags(id,name,name_key,search_text,status,version,created_by_user_id,updated_by_user_id,
created_at_ms,updated_at_ms,deleted_at_ms) VALUES(?,?,?,?,'ACTIVE',1,?,?,?,?,NULL)
`, id.String(), name, nameKey, searchText, actorUserID, actorUserID, now, now); err != nil {
			if strings.Contains(err.Error(), "tags_active_name_key") || strings.Contains(err.Error(), "UNIQUE constraint") {
				return ErrNameConflict
			}
			return fmt.Errorf("tagging: create tag: %w", err)
		}
		result, err = adminItemByID(ctx, connection, id.String())
		if err != nil {
			return err
		}
		return writeAudit(ctx, connection, actorUserID, "TAG_CREATED", "TAG", id.String(), nil, result, nil, now)
	})
	return result, err
}

func (service *Service) Rename(
	ctx context.Context,
	actorUserID, tagID, rawName string,
	expectedVersion int64,
) (AdminItem, error) {
	if !ValidID(actorUserID) || !ValidID(tagID) || expectedVersion < 1 {
		return AdminItem{}, ErrInvalid
	}
	name, nameKey, searchText, err := NormalizeName(rawName)
	if err != nil {
		return AdminItem{}, err
	}
	var result AdminItem
	err = service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		before, err := adminItemByID(ctx, connection, tagID)
		if err != nil {
			return err
		}
		if before.Status == StatusDeleted {
			return ErrAlreadyDeleted
		}
		if before.Version != expectedVersion {
			return ErrVersionConflict
		}
		if before.Name == name {
			return ErrInvalid
		}
		var conflict int
		if err := connection.QueryRowContext(ctx, `
SELECT count(*) FROM tags WHERE status='ACTIVE' AND name_key=? AND id<>?
`, nameKey, tagID).Scan(&conflict); err != nil {
			return fmt.Errorf("tagging: check rename: %w", err)
		}
		if conflict != 0 {
			return ErrNameConflict
		}
		now := service.now().UnixMilli()
		updated, err := connection.ExecContext(ctx, `
UPDATE tags SET name=?,name_key=?,search_text=?,version=version+1,updated_by_user_id=?,updated_at_ms=?
WHERE id=? AND status='ACTIVE' AND version=?
`, name, nameKey, searchText, actorUserID, now, tagID, expectedVersion)
		if err != nil {
			return fmt.Errorf("tagging: rename tag: %w", err)
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return ErrVersionConflict
		}
		result, err = adminItemByID(ctx, connection, tagID)
		if err != nil {
			return err
		}
		return writeAudit(ctx, connection, actorUserID, "TAG_RENAMED", "TAG", tagID, before, result,
			map[string]any{"name": map[string]string{"before": before.Name, "after": result.Name}}, now)
	})
	return result, err
}

func (service *Service) Delete(
	ctx context.Context,
	actorUserID, tagID, confirmName string,
	expectedVersion int64,
) (AdminItem, DeleteImpact, error) {
	if !ValidID(actorUserID) || !ValidID(tagID) || expectedVersion < 1 {
		return AdminItem{}, DeleteImpact{}, ErrInvalid
	}
	var result AdminItem
	var impact DeleteImpact
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		before, err := adminItemByID(ctx, connection, tagID)
		if err != nil {
			return err
		}
		if before.Status == StatusDeleted {
			return ErrAlreadyDeleted
		}
		if before.Version != expectedVersion {
			return ErrVersionConflict
		}
		if confirmName != before.Name {
			return ErrDeleteConfirmation
		}
		impact = DeleteImpact(before.Usage)
		now := service.now().UnixMilli()
		updated, err := connection.ExecContext(ctx, `
UPDATE tags SET status='DELETED',version=version+1,updated_by_user_id=?,updated_at_ms=?,deleted_at_ms=?
WHERE id=? AND status='ACTIVE' AND version=?
`, actorUserID, now, now, tagID, expectedVersion)
		if err != nil {
			return fmt.Errorf("tagging: delete tag: %w", err)
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return ErrVersionConflict
		}
		if _, err := connection.ExecContext(ctx, `
UPDATE games SET version=version+1,updated_at_ms=?
WHERE id IN (SELECT game_id FROM game_tags WHERE tag_id=?)
`, now, tagID); err != nil {
			return fmt.Errorf("tagging: advance games after delete: %w", err)
		}
		if _, err := connection.ExecContext(ctx, `
UPDATE review_drafts SET version=version+1,updated_at_ms=?
WHERE id IN (
  SELECT relation.review_draft_id FROM review_draft_tags relation
  JOIN review_drafts draft ON draft.id=relation.review_draft_id
  JOIN import_items item ON item.id=draft.import_item_id AND item.state='REVIEW_PENDING'
  WHERE relation.tag_id=?
)
`, now, tagID); err != nil {
			return fmt.Errorf("tagging: advance reviews after delete: %w", err)
		}
		if _, err := connection.ExecContext(ctx, `
UPDATE pegasus_imports
SET version=version+1,
    mapping_version=mapping_version+CASE WHEN state='AWAITING_MAPPING' THEN 1 ELSE 0 END,
    updated_at_ms=?
WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
AND id IN (
  SELECT collection.import_id FROM pegasus_collection_tags relation
  JOIN pegasus_import_collections collection ON collection.id=relation.collection_id
  WHERE relation.tag_id=?
)
`, now, tagID); err != nil {
			return fmt.Errorf("tagging: advance Pegasus plans after delete: %w", err)
		}
		result, err = adminItemByID(ctx, connection, tagID)
		if err != nil {
			return err
		}
		return writeAudit(ctx, connection, actorUserID, "TAG_DELETED", "TAG", tagID, before, result,
			map[string]any{"impact": impact}, now)
	})
	return result, impact, err
}

func encodedIDs(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func activeReferences(
	ctx context.Context,
	database executor,
	relationTable, ownerColumn, ownerID string,
) ([]Reference, error) {
	query := `SELECT tag.id,tag.name FROM ` + relationTable + ` relation
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.` + ownerColumn + `=? ORDER BY tag.name_key,tag.id`
	rows, err := database.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("tagging: query owner references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Reference, 0)
	for rows.Next() {
		var reference Reference
		if err := rows.Scan(&reference.TagID, &reference.Name); err != nil {
			return nil, fmt.Errorf("tagging: scan owner reference: %w", err)
		}
		result = append(result, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate owner references: %w", err)
	}
	return result, nil
}

func ValidateActiveReferences(
	ctx context.Context,
	database executor,
	tagIDs []string,
) ([]Reference, error) {
	validated, err := ValidateIDs(tagIDs)
	if err != nil {
		return nil, err
	}
	if len(validated) == 0 {
		return []Reference{}, nil
	}
	rows, err := database.QueryContext(ctx, `
SELECT id,name FROM tags
WHERE status='ACTIVE' AND id IN (SELECT value FROM json_each(?))
ORDER BY name_key,id
`, encodedIDs(validated))
	if err != nil {
		return nil, fmt.Errorf("tagging: validate references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Reference, 0, len(validated))
	found := make(map[string]struct{}, len(validated))
	for rows.Next() {
		var reference Reference
		if err := rows.Scan(&reference.TagID, &reference.Name); err != nil {
			return nil, fmt.Errorf("tagging: scan reference: %w", err)
		}
		found[reference.TagID] = struct{}{}
		result = append(result, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate references: %w", err)
	}
	if len(found) != len(validated) {
		invalid := make([]string, 0)
		for _, tagID := range validated {
			if _, exists := found[tagID]; !exists {
				invalid = append(invalid, tagID)
			}
		}
		sort.Strings(invalid)
		return nil, &InvalidReferencesError{IDs: invalid}
	}
	return result, nil
}

func sameReferences(left, right []Reference) bool {
	if len(left) != len(right) {
		return false
	}
	leftIDs := make([]string, len(left))
	rightIDs := make([]string, len(right))
	for index := range left {
		leftIDs[index] = left[index].TagID
		rightIDs[index] = right[index].TagID
	}
	sort.Strings(leftIDs)
	sort.Strings(rightIDs)
	return strings.Join(leftIDs, "\x00") == strings.Join(rightIDs, "\x00")
}

func referenceIDs(values []Reference) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.TagID)
	}
	return result
}

func referenceDiff(before, after []Reference) ([]Reference, []Reference) {
	added := make([]Reference, 0)
	removed := make([]Reference, 0)
	beforeByID := make(map[string]Reference, len(before))
	afterByID := make(map[string]Reference, len(after))
	for _, value := range before {
		beforeByID[value.TagID] = value
	}
	for _, value := range after {
		afterByID[value.TagID] = value
		if _, exists := beforeByID[value.TagID]; !exists {
			added = append(added, value)
		}
	}
	for _, value := range before {
		if _, exists := afterByID[value.TagID]; !exists {
			removed = append(removed, value)
		}
	}
	return added, removed
}

func (service *Service) ReplaceGameTags(
	ctx context.Context,
	actorUserID, gameID string,
	expectedVersion int64,
	tagIDs []string,
) (GameTagResult, error) {
	if !ValidID(actorUserID) || !ValidID(gameID) || expectedVersion < 1 {
		return GameTagResult{}, ErrInvalid
	}
	if _, err := ValidateIDs(tagIDs); err != nil {
		return GameTagResult{}, err
	}
	var result GameTagResult
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		var currentVersion int64
		queryErr := connection.QueryRowContext(
			ctx, `SELECT version FROM games WHERE id=?`, gameID,
		).Scan(&currentVersion)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return ErrGameNotFound
		}
		if queryErr != nil {
			return fmt.Errorf("tagging: read game version: %w", queryErr)
		}
		if currentVersion != expectedVersion {
			return ErrVersionConflict
		}
		desired, err := ValidateActiveReferences(ctx, connection, tagIDs)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		before, after, err := replaceOwnerReferences(
			ctx, connection, "game_tags", "game_id", gameID, actorUserID, desired, now,
		)
		if err != nil {
			return err
		}
		if sameReferences(before, after) {
			result = GameTagResult{GameID: gameID, Version: currentVersion, Tags: before}
			return nil
		}
		updated, err := connection.ExecContext(ctx, `
UPDATE games SET version=version+1,updated_at_ms=? WHERE id=? AND version=?
`, now, gameID, expectedVersion)
		if err != nil {
			return fmt.Errorf("tagging: advance game version: %w", err)
		}
		if affected, _ := updated.RowsAffected(); affected != 1 {
			return ErrVersionConflict
		}
		added, removed := referenceDiff(before, after)
		result = GameTagResult{GameID: gameID, Version: currentVersion + 1, Tags: after}
		return writeAudit(ctx, connection, actorUserID, "GAME_TAGS_REPLACED", "GAME", gameID,
			before, after, map[string]any{"added": added, "removed": removed}, now)
	})
	return result, err
}

func (service *Service) References(
	ctx context.Context,
	gameIDs []string,
) (map[string][]Reference, error) {
	result := make(map[string][]Reference, len(gameIDs))
	if len(gameIDs) == 0 {
		return result, nil
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT relation.game_id,tag.id,tag.name
FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id IN (SELECT value FROM json_each(?))
ORDER BY relation.game_id,tag.name_key,tag.id
`, encodedIDs(gameIDs))
	if err != nil {
		return nil, fmt.Errorf("tagging: query game references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var gameID string
		var reference Reference
		if err := rows.Scan(&gameID, &reference.TagID, &reference.Name); err != nil {
			return nil, fmt.Errorf("tagging: scan game reference: %w", err)
		}
		result[gameID] = append(result[gameID], reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate game references: %w", err)
	}
	return result, nil
}
