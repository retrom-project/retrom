package tagging

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/tagging"
)

func (records tagRecords) Insert(ctx context.Context, input tagging.TagWrite) error {
	if _, err := records.database.ExecContext(
		ctx,
		`
INSERT INTO tags(id,name,name_key,search_text,status,version,created_by_user_id,updated_by_user_id,
created_at_ms,updated_at_ms,deleted_at_ms) VALUES(?,?,?,?,'ACTIVE',1,?,?,?,?,NULL)
`,
		input.ID,
		input.Name,
		input.NameKey,
		input.SearchText,
		input.ActorUserID,
		input.ActorUserID,
		input.NowMS,
		input.NowMS,
	); err != nil {
		return fmt.Errorf("tagging: create common tag: %w", err)
	}
	return nil
}

func (records tagRecords) Rename(ctx context.Context, input tagging.TagWrite) error {
	updated, err := recordstore.UpdateTags(ctx, records.database, recordstore.Update{
		Set: `
name=?,name_key=?,search_text=?,version=version+1,updated_by_user_id=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND status='ACTIVE' AND version=?`,
			Args:  []any{input.ID, input.ExpectedVersion},
		},
		Values: []any{input.Name, input.NameKey, input.SearchText, input.ActorUserID, input.NowMS},
	})
	if err != nil {
		return fmt.Errorf("tagging: rename tag: %w", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return tagging.ErrVersionConflict
	}
	return nil
}

func (records tagRecords) Delete(ctx context.Context, input tagging.TagWrite) error {
	updated, err := recordstore.UpdateTags(ctx, records.database, recordstore.Update{
		Set: `
status='DELETED',version=version+1,updated_by_user_id=?,updated_at_ms=?,deleted_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND status='ACTIVE' AND version=?`,
			Args:  []any{input.ID, input.ExpectedVersion},
		},
		Values: []any{input.ActorUserID, input.NowMS, input.NowMS},
	})
	if err != nil {
		return fmt.Errorf("tagging: delete tag: %w", err)
	}
	if affected, _ := updated.RowsAffected(); affected != 1 {
		return tagging.ErrVersionConflict
	}
	if _, err := recordstore.UpdateGames(ctx, records.database, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id IN (SELECT game_id FROM game_tags WHERE tag_id=?)`,
			Args:  []any{input.ID},
		},
		Values: []any{input.NowMS},
	}); err != nil {
		return fmt.Errorf("tagging: advance games after delete: %w", err)
	}
	if _, err := recordstore.UpdateReviewDrafts(ctx, records.database, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `
id IN (
  SELECT relation.review_draft_id FROM review_draft_tags relation
  JOIN review_drafts draft ON draft.id=relation.review_draft_id
  JOIN import_items item ON item.id=draft.import_item_id AND item.state='REVIEW_PENDING'
  WHERE relation.tag_id=?
)
`,
			Args: []any{input.ID},
		},
		Values: []any{input.NowMS},
	}); err != nil {
		return fmt.Errorf("tagging: advance reviews after delete: %w", err)
	}
	if _, err := recordstore.UpdateSourceImports(ctx, records.database, recordstore.Update{
		Set: `
version=version+1,
    mapping_version=mapping_version+CASE WHEN state='AWAITING_MAPPING' THEN 1 ELSE 0 END,
    updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
AND id IN (
  SELECT collection.import_id FROM source_collection_tags relation
  JOIN source_import_collections collection ON collection.id=relation.collection_id
  WHERE relation.tag_id=?
)
`,
			Args: []any{input.ID},
		},
		Values: []any{input.NowMS},
	}); err != nil {
		return fmt.Errorf("tagging: advance Source plans after delete: %w", err)
	}
	return nil
}
