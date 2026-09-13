package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

type ReviewPreviewValidationDraft struct {
	TargetID string
	Selected string
	DOSEntry sql.NullString
}

type ReviewPreviewValidations struct{ executor dbexec.Executor }

// ReviewPreviewValidationRepository owns the transaction around preview
// validation refreshes. The validation resolver remains a typed callback until
// the legacy validation workflow is migrated into the application package.
type ReviewPreviewValidationRepository struct {
	database *sql.DB
	refresh  DraftValidationRefresher
}

func NewReviewPreviewValidationRepository(
	database *sql.DB,
	refresh DraftValidationRefresher,
) *ReviewPreviewValidationRepository {
	return &ReviewPreviewValidationRepository{database: database, refresh: refresh}
}

func (repository *ReviewPreviewValidationRepository) Refresh(
	ctx context.Context, itemID string, nowMS int64,
) error {
	if repository == nil || repository.refresh == nil {
		return application.ErrInvalid
	}
	return NewTransactions(repository.database).Write(ctx, func(executor dbexec.Executor) error {
		draft, err := BindReviewPreviewValidations(executor).Draft(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read review preview draft: %w", err)
		}
		validationID, err := repository.refresh(ctx, executor, itemID, draft.TargetID, draft.DOSEntry)
		if err != nil {
			return err
		}
		if validationID == draft.Selected {
			return nil
		}
		if err := BindReviewPreviewValidations(executor).Select(ctx, itemID, validationID, nowMS); err != nil {
			return fmt.Errorf("select review preview validation: %w", err)
		}
		return nil
	})
}

func BindReviewPreviewValidations(executor dbexec.Executor) ReviewPreviewValidations {
	return ReviewPreviewValidations{executor: executor}
}

func (records ReviewPreviewValidations) Draft(
	ctx context.Context, itemID string,
) (ReviewPreviewValidationDraft, error) {
	var result ReviewPreviewValidationDraft
	err := records.executor.QueryRowContext(ctx, `
SELECT draft.target_platform_instance_id,COALESCE(draft.selected_validation_id,''),draft.default_dos_entry
FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&result.TargetID, &result.Selected, &result.DOSEntry)
	if err != nil {
		return ReviewPreviewValidationDraft{}, fmt.Errorf("query review preview draft: %w", err)
	}
	return result, nil
}

func (records ReviewPreviewValidations) Select(
	ctx context.Context, itemID, validationID string, now int64,
) error {
	_, err := recordstore.UpdateReviewDrafts(ctx, records.executor, recordstore.Update{
		Set:    `selected_validation_id=NULLIF(?,''),version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `import_item_id=?`, Args: []any{itemID}},
		Values: []any{validationID, now},
	})
	if err != nil {
		return fmt.Errorf("select review preview validation: %w", err)
	}
	return nil
}

var _ application.ReviewPreviewValidationRepository = (*ReviewPreviewValidationRepository)(nil)
