package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/recordstore"
)

// closeReview transfers authority in the same transaction as the terminal decision.
// Payload removal remains owned by the recoverable cleanup job.
func closeReview(ctx context.Context, executor dbapi.Executor, itemID string, now int64) error {
	if err := (itemrelease.Records{Executor: executor}).RevokePreviews(ctx, itemID, now); err != nil {
		return fmt.Errorf("close completed review previews: %w", err)
	}
	if _, err := recordstore.DeleteRows(ctx, executor, "review_draft_tags",
		recordstore.Scope{Where: "review_draft_id=?", Args: []any{itemID}}); err != nil {
		return fmt.Errorf("detach completed review tags: %w", err)
	}
	if _, err := recordstore.UpdateReviewItems(ctx, executor, recordstore.Update{
		Set: "review_profile_json=NULL", Scope: recordstore.Scope{Where: "id=?", Args: []any{itemID}},
	}); err != nil {
		return fmt.Errorf("detach completed review profile: %w", err)
	}
	return nil
}
