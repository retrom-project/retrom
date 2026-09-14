package tagging

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

func ownerTable(kind tagging.OwnerKind) (string, string, error) {
	switch kind {
	case tagging.OwnerGame:
		return "game_tags", "game_id", nil
	case tagging.OwnerReviewDraft:
		return "review_draft_tags", "review_draft_id", nil
	case tagging.OwnerPegasusCollection:
		return "pegasus_collection_tags", "collection_id", nil
	case tagging.OwnerEmulationStationCollection:
		return "emulationstation_collection_tags", "collection_id", nil
	case tagging.OwnerReviewItem:
		return "", "", tagging.ErrInvalid
	default:
		return "", "", tagging.ErrInvalid
	}
}

func (records relationRecords) References(ctx context.Context, owner tagging.Owner) ([]tagging.Reference, error) {
	table, column, err := ownerTable(owner.Kind)
	if err != nil {
		return nil, err
	}
	return activeReferences(ctx, records.database, table, column, owner.ID)
}

func (records relationRecords) Add(ctx context.Context, input tagging.Assignment) error {
	table, column, err := ownerTable(input.Owner.Kind)
	if err != nil {
		return err
	}
	for _, reference := range input.References {
		query := `INSERT INTO ` + table + `(` + column + `,tag_id,assigned_by_user_id,created_at_ms) VALUES(?,?,?,?)`
		if _, err := createOwnerTag(
			ctx,
			records.database,
			table,
			query,
			input.Owner.ID,
			reference.TagID,
			input.ActorUserID,
			input.NowMS,
		); err != nil {
			return err
		}
	}
	return nil
}

func (records relationRecords) Remove(ctx context.Context, owner tagging.Owner, tagIDs []string) error {
	table, column, err := ownerTable(owner.Kind)
	if err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := deleteOwnerTag(
			ctx,
			records.database,
			table,
			recordstore.Scope{
				Where: column + "=? AND tag_id=?",
				Args: []any{
					owner.ID,
					tagID,
				},
			},
		); err != nil {
			return err
		}
	}
	return nil
}

func (records relationRecords) TouchTags(ctx context.Context, actorUserID string, tagIDs []string, now int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	_, err := recordstore.UpdateTags(ctx, records.database, recordstore.Update{
		Set: `version=version+1,updated_by_user_id=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `status='ACTIVE' AND id IN (SELECT value FROM json_each(?))`,
			Args: []any{
				encodedIDs(
					tagIDs,
				),
			},
		},
		Values: []any{actorUserID, now},
	})
	if err != nil {
		return fmt.Errorf("tagging: advance owner tag usage: %w", err)
	}
	return nil
}

func encodedIDs(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func activeReferences(
	ctx context.Context,
	database dbexec.Executor,
	relationTable, ownerColumn, ownerID string,
) ([]tagging.Reference, error) {
	query := `SELECT tag.id,tag.name FROM ` + relationTable + ` relation
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.` + ownerColumn + `=? ORDER BY tag.name_key,tag.id`
	rows, err := database.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("tagging: query owner references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]tagging.Reference, 0)
	for rows.Next() {
		var reference tagging.Reference
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

func createOwnerTag(ctx context.Context, db dbexec.Executor, table, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	var err error

	switch table {
	case "game_tags":
		result, err = db.ExecContext(ctx, query, args...)
	case "review_draft_tags":
		result, err = recordstore.CreateReviewDraftTags(ctx, db, query, args...)
	case "pegasus_collection_tags":
		result, err = recordstore.CreatePegasusCollectionTags(ctx, db, query, args...)
	case "emulationstation_collection_tags":
		result, err = recordstore.CreateEmulationstationCollectionTags(ctx, db, query, args...)
	default:
		return nil, tagging.ErrInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("tagging owner relation: %w", err)
	}
	return result, nil
}

func deleteOwnerTag(
	ctx context.Context,
	db dbexec.Executor,
	table string,
	scope recordstore.Scope,
) (sql.Result, error) {
	var result sql.Result
	var err error

	switch table {
	case "review_draft_tags":
		result, err = recordstore.DeleteReviewDraftTags(ctx, db, scope)
	case "pegasus_collection_tags":
		result, err = recordstore.DeletePegasusCollectionTags(ctx, db, scope)
	case "emulationstation_collection_tags":
		result, err = recordstore.DeleteEmulationstationCollectionTags(ctx, db, scope)
	case "game_tags":
		result, err = db.ExecContext(ctx, "DELETE FROM game_tags WHERE "+scope.Where, scope.Args...)
	default:
		return nil, tagging.ErrInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("tagging owner relation: %w", err)
	}
	return result, nil
}
