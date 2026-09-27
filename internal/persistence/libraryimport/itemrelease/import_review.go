package itemrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	"retrom/internal/persistence/sessionstore"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) ClearReview(ctx context.Context, itemID string, now int64) error {
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "review_preview_sessions",
		sessionstore.ChangePreview, recordstore.Update{
			Set: `
state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1
`, Scope: recordstore.Scope{
				Where: `import_item_id=? AND state IN ('CREATED','ACTIVE','FINISHED')`,
				Args:  []any{itemID},
			},
			Values: []any{now, now},
		}); err != nil {
		return fmt.Errorf("payloadrelease/revoke review preview: %w", err)
	}
	if err := (releaseops.Records{Executor: records.Executor}).ExecUpdate(
		ctx,
		"review_draft_screenshot_assets",
		`rowid IN (SELECT rowid FROM review_draft_screenshot_assets WHERE review_draft_id=? ORDER BY rowid LIMIT 200)`,
		[]any{itemID},
		`
DELETE FROM review_draft_screenshot_assets WHERE rowid IN
 (SELECT rowid FROM review_draft_screenshot_assets WHERE review_draft_id=? ORDER BY rowid LIMIT 200)
`,
		itemID,
	); err != nil {
		return fmt.Errorf("payloadrelease/clear review screenshots: %w", err)
	}
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "import_items",
		recordstore.UpdateReviewItems, recordstore.Update{
			Set: `
cover_candidate_asset_id=NULL,background_candidate_asset_id=NULL,cover_uploaded_asset_id=NULL,
review_version=CASE WHEN review_version>0 THEN review_version+1 ELSE 0 END,
review_updated_at_ms=CASE WHEN review_version>0 THEN ? ELSE NULL END
`,
			Scope: recordstore.Scope{
				Where: `id=? AND (cover_candidate_asset_id IS NOT NULL OR background_candidate_asset_id IS NOT NULL
 OR cover_uploaded_asset_id IS NOT NULL)`,
				Args: []any{itemID},
			},
			Values: []any{now},
		}); err != nil {
		return fmt.Errorf("payloadrelease/clear review draft: %w", err)
	}
	return nil
}
