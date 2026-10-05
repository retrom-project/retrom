package itemrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/filedeletion"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	"retrom/internal/persistence/sessionstore"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) ClearReview(ctx context.Context, itemID string, now int64) error {
	if err := records.RevokePreviews(ctx, itemID, now); err != nil {
		return err
	}
	if err := (releaseops.Records{Executor: records.Executor}).ExecUpdate(
		ctx,
		"review_draft_screenshot_assets",
		`ctid IN (SELECT ctid FROM review_draft_screenshot_assets WHERE review_draft_id=? ORDER BY ctid LIMIT 200)`,
		[]any{itemID},
		`
DELETE FROM review_draft_screenshot_assets WHERE ctid IN
 (SELECT ctid FROM review_draft_screenshot_assets WHERE review_draft_id=? ORDER BY ctid LIMIT 200)
`,
		itemID,
	); err != nil {
		return fmt.Errorf("cleanupjobs/clear review screenshots: %w", err)
	}
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "import_items",
		recordstore.UpdateReviewItems, recordstore.Update{
			Set: `
cover_candidate_asset_id=NULL,background_candidate_asset_id=NULL,cover_uploaded_asset_id=NULL,
video_uploaded_asset_id=NULL,
review_version=CASE WHEN review_version>0 THEN review_version+1 ELSE 0 END,
review_updated_at_ms=CASE WHEN review_version>0 THEN ?::bigint ELSE NULL END
`,
			Scope: recordstore.Scope{
				Where: `id=? AND (cover_candidate_asset_id IS NOT NULL OR background_candidate_asset_id IS NOT NULL
 OR cover_uploaded_asset_id IS NOT NULL OR video_uploaded_asset_id IS NOT NULL)`,
				Args: []any{itemID},
			},
			Values: []any{now},
		}); err != nil {
		return fmt.Errorf("cleanupjobs/clear review draft: %w", err)
	}
	return nil
}

func (records Records) RevokePreviews(ctx context.Context, itemID string, now int64) error {
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "runtime_preview_sessions",
		sessionstore.ChangePreview, recordstore.Update{
			Set: `
state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1
`, Scope: recordstore.Scope{
				Where: `id IN (SELECT preview_session_id FROM review_preview_bindings WHERE import_item_id=?)
 AND state IN ('CREATED','ACTIVE','FINISHED')`,
				Args: []any{itemID},
			},
			Values: []any{now, now},
		}); err != nil {
		return fmt.Errorf("cleanupjobs/revoke review preview: %w", err)
	}
	ids, err := dbapi.QueryStrings(ctx, records.Executor, `SELECT session.id
 FROM runtime_preview_sessions session JOIN review_preview_bindings binding ON binding.preview_session_id=session.id
 WHERE binding.import_item_id=? AND session.state IN ('EXPIRED','REVOKED')`, itemID)
	if err != nil {
		return fmt.Errorf("read retired preview directories: %w", err)
	}
	for _, id := range ids {
		if err := filedeletion.QueuePath(ctx, records.Executor, "previews/"+id, now); err != nil {
			return fmt.Errorf("queue retired preview directory: %w", err)
		}
	}
	return nil
}
