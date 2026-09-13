package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/repo/sessionstore"
)

func (records effectRecords) clearImportReview(ctx context.Context, itemID string, now int64) error {
	if err := records.checkedUpdate(ctx, "review_preview_sessions", sessionstore.ChangePreview, recordstore.Update{
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
	if err := records.execUpdate(
		ctx,
		"review_draft_screenshot_assets",
		`review_draft_id IN (SELECT id FROM review_drafts WHERE import_item_id=?)`,
		[]any{itemID},
		`
DELETE FROM review_draft_screenshot_assets WHERE review_draft_id IN (
  SELECT id FROM review_drafts WHERE import_item_id=?
)
`,
		itemID,
	); err != nil {
		return fmt.Errorf("payloadrelease/clear review screenshots: %w", err)
	}
	if err := records.checkedUpdate(ctx, "review_drafts", recordstore.UpdateReviewDrafts, recordstore.Update{
		Set: `
cover_candidate_asset_id=NULL,background_candidate_asset_id=NULL,cover_uploaded_asset_id=NULL,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `import_item_id=?`,
			Args:  []any{itemID},
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("payloadrelease/clear review draft: %w", err)
	}
	return nil
}
