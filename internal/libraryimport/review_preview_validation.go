package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

// RefreshReviewPreviewValidation resolves current runtime dependencies without
// resubmitting metadata. Imported metadata may exceed the editor's write limits.
func (service *Service) RefreshReviewPreviewValidation(ctx context.Context, itemID string) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("refresh review preview validation: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var targetID, selected string
	var dosEntry sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT draft.target_platform_instance_id,COALESCE(draft.selected_validation_id,''),draft.default_dos_entry
FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&targetID, &selected, &dosEntry)
	if err != nil {
		return fmt.Errorf("read review preview draft: %w", err)
	}
	validationID, err := service.ensureCompatibleDraftValidation(ctx, transaction, itemID, targetID, dosEntry)
	if err != nil {
		return err
	}
	if validationID != selected {
		_, err = transaction.ExecContext(ctx, `
UPDATE review_drafts SET selected_validation_id=NULLIF(?,''),version=version+1,updated_at_ms=?
WHERE import_item_id=?
`, validationID, service.now().UnixMilli(), itemID)
		if err != nil {
			return fmt.Errorf("select review preview validation: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review preview validation: %w", err)
	}
	return nil
}
