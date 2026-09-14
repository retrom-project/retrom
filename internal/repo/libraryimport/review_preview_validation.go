package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

// ReviewPreviewValidationRepository owns the preview refresh transaction. It
// accepts a value-only plan and never invokes a service or adapter callback.
type ReviewPreviewValidationRepository struct{ database *sql.DB }

func NewReviewPreviewValidationRepository(database *sql.DB) *ReviewPreviewValidationRepository {
	return &ReviewPreviewValidationRepository{database: database}
}

func (repository *ReviewPreviewValidationRepository) Draft(
	ctx context.Context, itemID string,
) (application.ReviewPreviewValidationDraft, error) {
	if repository == nil || repository.database == nil {
		return application.ReviewPreviewValidationDraft{}, application.ErrInvalid
	}
	var result application.ReviewPreviewValidationDraft
	var dosEntry sql.NullString
	err := repository.database.QueryRowContext(ctx, `
SELECT draft.target_platform_instance_id,COALESCE(draft.selected_validation_id,''),
  draft.default_dos_entry,draft.version
FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&result.TargetID, &result.Selected, &dosEntry, &result.Version)
	if err != nil {
		return application.ReviewPreviewValidationDraft{}, fmt.Errorf("query review preview draft: %w", err)
	}
	result.ItemID = itemID
	result.DOSEntry = nullablePointer(dosEntry)
	return result, nil
}

func (repository *ReviewPreviewValidationRepository) Commit(
	ctx context.Context, plan application.ReviewPreviewValidationPlan,
) error {
	if repository == nil || repository.database == nil || plan.ItemID == "" || plan.ValidationID == "" ||
		plan.ExpectedVersion < 1 || plan.NowMS < 0 {
		return application.ErrInvalid
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review preview validation: %w", err)
	}
	defer dbexec.Rollback(transaction)
	guard := plan.ExpectedValidationGuard
	inputs, err := BindReviewValidation(transaction).Inputs(ctx, plan.ItemID, guard.TargetPlatformInstanceID)
	if err != nil {
		return fmt.Errorf("read review preview validation facts: %w", err)
	}
	if !reviewValidationGuardMatches(guard, inputs, guard.TargetPlatformInstanceID, guard.DefaultDOSEntry) {
		return application.ErrVersionConflict
	}
	if err := createPreviewValidation(ctx, transaction, plan); err != nil {
		return err
	}
	if err := selectPreviewValidation(ctx, transaction, plan); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review preview validation: %w", err)
	}
	return nil
}

func createPreviewValidation(
	ctx context.Context, transaction *sql.Tx, plan application.ReviewPreviewValidationPlan,
) error {
	if plan.Create == nil {
		return nil
	}
	if !previewValidationCreateMatches(plan) {
		return application.ErrInvalid
	}
	if err := BindReviewValidation(transaction).Create(ctx, *plan.Create); err != nil {
		return fmt.Errorf("create review preview validation: %w", err)
	}
	if err := BindReviewValidation(transaction).CopyFiles(ctx, *plan.Copy); err != nil {
		return fmt.Errorf("copy review preview validation files: %w", err)
	}
	return nil
}

func previewValidationCreateMatches(plan application.ReviewPreviewValidationPlan) bool {
	create := plan.Create
	if create.ID != plan.ValidationID || create.ItemID != plan.ItemID || plan.Copy == nil {
		return false
	}
	if plan.Copy.ValidationID != plan.ValidationID {
		return false
	}
	guard := plan.ExpectedValidationGuard
	if create.TargetPlatformInstanceID != guard.TargetPlatformInstanceID ||
		create.PlatformInstanceVersion != guard.PlatformInstanceVersion {
		return false
	}
	if create.CoreID != guard.CoreID || create.ProviderID != guard.ProviderID || create.TargetID != guard.TargetID {
		return false
	}
	if create.SourceSnapshotID != guard.SourceSnapshotID || create.SourceManifestDigest != guard.SourceManifestDigest {
		return false
	}
	if !sameNullable(create.DATVersionID, guard.DATVersionID) {
		return false
	}
	if create.PrepublishInputDigest != plan.ExpectedSelectedValidationPrepublishDigest {
		return false
	}
	return sameNullable(create.DefaultDOSEntry, guard.DefaultDOSEntry)
}

func selectPreviewValidation(
	ctx context.Context, transaction *sql.Tx, plan application.ReviewPreviewValidationPlan,
) error {
	result, err := recordstore.UpdateReviewDrafts(ctx, transaction, recordstore.Update{
		Set: `selected_validation_id=NULLIF(?,''),version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `import_item_id=? AND version=? AND
  COALESCE(selected_validation_id,'')=? AND EXISTS(
    SELECT 1 FROM import_items item WHERE item.id=review_drafts.import_item_id AND item.state='REVIEW_PENDING'
  ) AND EXISTS(
    SELECT 1 FROM import_item_core_validations validation
    WHERE validation.id=? AND validation.import_item_id=review_drafts.import_item_id AND validation.status='READY'
  AND validation.prepublish_input_digest=?
  )`, Args: []any{
			plan.ItemID, plan.ExpectedVersion, plan.ExpectedSelectedValidation,
			plan.ValidationID, plan.ExpectedSelectedValidationPrepublishDigest,
		}},
		Values: []any{plan.ValidationID, plan.NowMS},
	})
	if err != nil {
		return fmt.Errorf("select review preview validation: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return application.ErrVersionConflict
	}
	return nil
}

// The binder remains useful to other read-side review code. It intentionally
// exposes only executor-scoped SQL helpers, not the application transaction.
type ReviewPreviewValidations struct{ executor dbexec.Executor }

func BindReviewPreviewValidations(executor dbexec.Executor) ReviewPreviewValidations {
	return ReviewPreviewValidations{executor: executor}
}

func (records ReviewPreviewValidations) Draft(
	ctx context.Context, itemID string,
) (application.ReviewPreviewValidationDraft, error) {
	var result application.ReviewPreviewValidationDraft
	var dosEntry sql.NullString
	err := records.executor.QueryRowContext(ctx, `
SELECT draft.target_platform_instance_id,COALESCE(draft.selected_validation_id,''),
  draft.default_dos_entry,draft.version
FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&result.TargetID, &result.Selected, &dosEntry, &result.Version)
	if err != nil {
		return application.ReviewPreviewValidationDraft{}, fmt.Errorf("query review preview draft: %w", err)
	}
	result.ItemID = itemID
	result.DOSEntry = nullablePointer(dosEntry)
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
