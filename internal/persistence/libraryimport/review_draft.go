package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

type (
	MetadataPatch  = application.MetadataPatch
	SelectedAssets = application.SelectedAssets
	DraftPatch     = application.DraftPatch
	DraftResult    = application.DraftResult
)

// DraftValidationRefresher is supplied by the legacy validation subsystem
// until that subsystem completes its own repository extraction. It receives
// only the common executor contract, so this repository still owns the
// transaction and all draft writes.
type DraftValidationRefresher func(
	context.Context, dbexec.Executor, string, string, sql.NullString,
) (string, error)

type ScummVMSelector func(
	context.Context, dbexec.Executor, string, string, sql.NullString, string,
) (string, error)

type ReviewDraftPatchOptions struct {
	Tags              *tagging.Service
	Now               func() time.Time
	RefreshValidation DraftValidationRefresher
	SelectScummVM     ScummVMSelector
}

type ReviewDraftPatches struct {
	database          *sql.DB
	tags              *tagging.Service
	now               func() time.Time
	refreshValidation DraftValidationRefresher
	selectScummVM     ScummVMSelector
}

func NewReviewDraftPatches(database *sql.DB, options ReviewDraftPatchOptions) *ReviewDraftPatches {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &ReviewDraftPatches{
		database: database, tags: options.Tags, now: now,
		refreshValidation: options.RefreshValidation, selectScummVM: options.SelectScummVM,
	}
}

// Contract branches stay contiguous for a single auditable decision.
func (repository *ReviewDraftPatches) Patch(
	ctx context.Context, request application.ReviewDraftPatchRequest,
) (application.DraftResult, error) {
	if err := application.ValidateDraftPatch(request.Patch); err != nil {
		return application.DraftResult{}, fmt.Errorf("validate review draft patch: %w", err)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("begin review draft patch: %w", err)
	}
	defer dbexec.Rollback(transaction)
	run := draftPatchRun{
		repository: repository, ctx: ctx, transaction: transaction,
		itemID: request.ItemID, expectedVersion: request.ExpectedVersion, patch: request.Patch,
		actor: request.Actor,
	}
	if err := run.load(); err != nil {
		return application.DraftResult{}, err
	}
	if err := run.applyChanges(); err != nil {
		return application.DraftResult{}, err
	}
	return run.persist()
}

type draftPatchRun struct {
	repository          *ReviewDraftPatches
	ctx                 context.Context
	transaction         *sql.Tx
	itemID              string
	expectedVersion     int64
	patch               application.DraftPatch
	actor               authn.Actor
	draftID             string
	targetID            string
	validationID        string
	effectiveSnapshotID string
	metadataJSON        string
	candidateID         sql.NullString
	coverID             sql.NullString
	uploadedCoverID     sql.NullString
	backgroundID        sql.NullString
	dosEntry            sql.NullString
	metadata            map[string]any
	targetOrDOSChanged  bool
	isRPG               bool
}

func (run *draftPatchRun) load() error {
	var currentVersion int64
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT d.id,d.target_platform_instance_id,COALESCE(d.selected_validation_id,''),
  d.effective_source_snapshot_id,d.selected_candidate_id,d.cover_candidate_asset_id,
  d.cover_uploaded_asset_id,d.background_candidate_asset_id,d.default_dos_entry,
  d.metadata_json,d.review_version,
  EXISTS(SELECT 1 FROM rpgmaker_review_profiles profile WHERE profile.review_draft_id=d.id)
FROM import_items i
JOIN import_items d ON d.id=i.id
WHERE i.id=? AND i.state='REVIEW_PENDING'
`, run.itemID).Scan(
		&run.draftID, &run.targetID, &run.validationID, &run.effectiveSnapshotID,
		&run.candidateID, &run.coverID, &run.uploadedCoverID, &run.backgroundID,
		&run.dosEntry, &run.metadataJSON, &currentVersion, &run.isRPG,
	)
	if err != nil {
		return application.ErrInvalid
	}
	if currentVersion != run.expectedVersion {
		return application.ErrVersionConflict
	}
	if err := json.Unmarshal([]byte(run.metadataJSON), &run.metadata); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func (run *draftPatchRun) applyChanges() error {
	steps := []func() error{
		run.applyMetadata, run.applyTarget, run.applySelectedValidation,
		run.applySelectedCandidate, run.applyDefaultDOSEntry, run.applyRPGMakerBinding,
		run.refreshValidation, run.applyScummVMSelection,
		run.applySelectedAssets,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	if run.targetOrDOSChanged && run.validationID == "" {
		return application.ErrInvalid
	}
	return nil
}

func (run *draftPatchRun) applyMetadata() error {
	if run.patch.Metadata == nil {
		return nil
	}
	updates, err := run.validatedMetadataUpdates(*run.patch.Metadata)
	if err != nil {
		return err
	}
	for key, value := range updates {
		run.metadata[key] = value
	}
	return nil
}

func (run *draftPatchRun) validatedMetadataUpdates(patch MetadataPatch) (map[string]any, error) {
	updates := make(map[string]any)
	if patch.Title != nil {
		if !application.ValidReviewField(*patch.Title, 200, false) || *patch.Title == "" {
			return nil, application.ErrInvalid
		}
		updates["title"] = *patch.Title
	}
	fields := []struct {
		key       string
		value     *string
		maximum   int
		multiline bool
	}{
		{key: "description", value: patch.Description, maximum: 10_000, multiline: true},
		{key: "developer", value: patch.Developer, maximum: 200},
		{key: "publisher", value: patch.Publisher, maximum: 200},
		{key: "genre", value: patch.Genre, maximum: 200},
	}
	for _, field := range fields {
		if field.value != nil && !application.ValidReviewField(*field.value, field.maximum, field.multiline) {
			return nil, application.ErrInvalid
		}
		if field.value != nil {
			updates[field.key] = *field.value
		}
	}
	if err := validateMetadataNumbers(run.repository, patch, updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func validateMetadataNumbers(
	repository *ReviewDraftPatches, patch application.MetadataPatch, updates map[string]any,
) error {
	playersPresent, players := patch.Players.Optional()
	if playersPresent {
		if players != nil && (*players < 1 || *players > 64) {
			return application.ErrInvalid
		}
		updates["players"] = nullablePatchInt(players)
	}
	releaseYearPresent, releaseYear := patch.ReleaseYear.Optional()
	if releaseYearPresent {
		maximumYear := int64(repository.now().UTC().Year() + 1)
		if releaseYear != nil && (*releaseYear < 1950 || *releaseYear > maximumYear) {
			return application.ErrInvalid
		}
		updates["releaseYear"] = nullablePatchInt(releaseYear)
	}
	return nil
}

func (run *draftPatchRun) applyTarget() error {
	if run.patch.TargetPlatformInstanceID == nil {
		return nil
	}
	var currentPlatform, targetPlatform string
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT platform_id FROM platform_instances WHERE id=?
`, run.targetID).Scan(&currentPlatform); err != nil {
		return application.ErrInvalid
	}
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT platform_id FROM platform_instances
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL
`, *run.patch.TargetPlatformInstanceID).Scan(&targetPlatform); err != nil {
		return application.ErrInvalid
	}
	if currentPlatform != targetPlatform {
		return application.ErrReimportRequiredPlatformChange
	}
	run.targetOrDOSChanged = run.targetID != *run.patch.TargetPlatformInstanceID
	run.targetID = *run.patch.TargetPlatformInstanceID
	return nil
}

func (run *draftPatchRun) applySelectedValidation() error {
	if run.patch.SelectedValidationID == nil {
		return nil
	}
	if run.isRPG {
		return application.ErrInvalid
	}
	var targetID, snapshotID, status string
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT target_platform_instance_id,source_snapshot_id,status
FROM import_item_core_validations
WHERE id=? AND import_item_id=?
`, *run.patch.SelectedValidationID, run.itemID).Scan(&targetID, &snapshotID, &status)
	if err != nil || targetID != run.targetID || snapshotID != run.effectiveSnapshotID || status != "READY" {
		return application.ErrInvalid
	}
	run.validationID = *run.patch.SelectedValidationID
	return nil
}

func (run *draftPatchRun) applySelectedCandidate() error {
	present, selectedCandidate := run.patch.SelectedCandidateID.Optional()
	if !present {
		return nil
	}
	if selectedCandidate == nil {
		run.candidateID = sql.NullString{}
		return nil
	}
	var count int
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE c.id=? AND r.import_item_id=? AND r.state='COMPLETED'
	`, *selectedCandidate, run.itemID).Scan(&count)
	if err != nil || count != 1 {
		return application.ErrInvalid
	}
	run.candidateID = sql.NullString{String: *selectedCandidate, Valid: true}
	return nil
}

func (run *draftPatchRun) applyDefaultDOSEntry() error {
	present, defaultEntry := run.patch.DefaultDOSEntry.Optional()
	if !present {
		return nil
	}
	previous := nullable(run.dosEntry)
	if defaultEntry == nil {
		run.dosEntry = sql.NullString{}
	} else {
		var count int
		err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*) FROM import_item_dos_entries
WHERE import_item_id=? AND normalized_path=? AND enabled=1
	`, run.itemID, *defaultEntry).Scan(&count)
		if err != nil || count != 1 {
			return application.ErrInvalid
		}
		run.dosEntry = sql.NullString{String: *defaultEntry, Valid: true}
	}
	run.targetOrDOSChanged = run.targetOrDOSChanged || previous != nullable(run.dosEntry)
	return nil
}

func (run *draftPatchRun) refreshValidation() error {
	if run.patch.SelectedValidationID != nil {
		return nil
	}
	if run.repository.refreshValidation == nil {
		return application.ErrInvalid
	}
	validationID, err := run.repository.refreshValidation(
		run.ctx, run.transaction, run.itemID, run.targetID, run.dosEntry,
	)
	if err != nil {
		return err
	}
	run.validationID = validationID
	return nil
}

func (run *draftPatchRun) applySelectedAssets() error {
	if run.patch.SelectedAssets == nil {
		return nil
	}
	assets := *run.patch.SelectedAssets
	if err := run.validateSelectedAssets(assets); err != nil {
		return err
	}
	run.coverID = nullableCandidate(assets.CoverCandidateAssetID)
	run.uploadedCoverID = nullableCandidate(assets.CoverUploadedAssetID)
	run.backgroundID = nullableCandidate(assets.BackgroundCandidateAssetID)
	if run.coverID.Valid && run.uploadedCoverID.Valid {
		return application.ErrInvalid
	}
	if run.uploadedCoverID.Valid &&
		!run.repository.validUploadedAsset(run.ctx, run.transaction, run.itemID, run.uploadedCoverID.String) {
		return application.ErrInvalid
	}
	for _, assetID := range []sql.NullString{run.coverID, run.backgroundID} {
		if assetID.Valid &&
			!run.repository.validCandidateAsset(run.ctx, run.transaction, run.itemID, assetID.String) {
			return application.ErrInvalid
		}
	}
	return run.replaceScreenshots(assets.ScreenshotCandidateAssetIDs)
}

func (run *draftPatchRun) validateSelectedAssets(assets SelectedAssets) error {
	if len(assets.ScreenshotCandidateAssetIDs) > 32 {
		return application.ErrInvalid
	}
	selected := make(map[string]struct{}, len(assets.ScreenshotCandidateAssetIDs))
	for _, assetID := range assets.ScreenshotCandidateAssetIDs {
		_, duplicate := selected[assetID]
		if duplicate || !run.repository.validCandidateAsset(run.ctx, run.transaction, run.itemID, assetID) {
			return application.ErrInvalid
		}
		selected[assetID] = struct{}{}
	}
	return nil
}

func (run *draftPatchRun) replaceScreenshots(assetIDs []string) error {
	_, err := run.transaction.ExecContext(run.ctx, `
DELETE FROM review_draft_screenshot_assets
WHERE review_draft_id=?
`, run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	for ordinal, assetID := range assetIDs {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_draft_screenshot_assets(
  review_draft_id,ordinal,candidate_asset_id,created_at_ms
)
SELECT id,?,?,? FROM import_items WHERE id=?
`, ordinal, assetID, run.repository.now().UnixMilli(), run.itemID)
		if err != nil {
			return fmt.Errorf("libraryimport/review: %w", err)
		}
	}
	return nil
}

func (run *draftPatchRun) persist() (application.DraftResult, error) {
	encoded, err := json.Marshal(run.metadata)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	searchParts, err := run.searchParts()
	if err != nil {
		return application.DraftResult{}, err
	}
	now := run.repository.now().UnixMilli()
	actor := run.actor
	actorUserID, _ := actor.UserID.(string)
	_, afterTags, err := run.repository.tags.ReplaceReviewDraftTags(
		run.ctx, tagpersistence.Bind(run.transaction), run.draftID, run.patch.TagIDs, actorUserID, now,
	)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: replace draft tags: %w", err)
	}
	if err := run.updateDraft(encoded, searchParts, now); err != nil {
		return application.DraftResult{}, err
	}
	if err := run.transaction.Commit(); err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return application.DraftResult{
		ItemID: run.itemID, Version: run.expectedVersion + 1,
		Metadata: run.metadata, Tags: afterTags, UpdatedAtMS: now,
	}, nil
}

func (run *draftPatchRun) searchParts() ([]string, error) {
	parts := []string{run.itemID}
	rows, err := run.transaction.QueryContext(run.ctx, `
SELECT u.relative_path
FROM import_item_source_snapshot_files s
JOIN import_files u ON u.id=s.upload_file_id
WHERE s.source_snapshot_id=?
ORDER BY s.sort_order,s.role,s.logical_name
`, run.itemID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			return nil, fmt.Errorf("libraryimport/review: %w", err)
		}
		parts = append(parts, filePath)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/review: %w", err)
	}
	if title, ok := run.metadata["title"].(string); ok {
		parts = append(parts, title)
	}
	return parts, nil
}

func (run *draftPatchRun) updateDraft(encoded []byte, searchParts []string, now int64) error {
	result, err := recordstore.UpdateReviewItems(run.ctx, run.transaction, recordstore.Update{
		Set: `
target_platform_instance_id=?,selected_validation_id=NULLIF(?,''),
  selected_candidate_id=?,cover_candidate_asset_id=?,cover_uploaded_asset_id=?,
  background_candidate_asset_id=?,default_dos_entry=?,metadata_json=?,
  review_version=review_version+1,review_updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND review_version=?`,
			Args:  []any{run.itemID, run.expectedVersion},
		},
		Values: []any{
			run.targetID,
			run.validationID,
			nullable(run.candidateID),
			nullable(run.coverID),
			nullable(run.uploadedCoverID),
			nullable(run.backgroundID),
			nullable(run.dosEntry),
			string(encoded),
			now,
		},
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return application.ErrVersionConflict
	}
	_, err = recordstore.UpdateImportItems(run.ctx, run.transaction, recordstore.Update{
		Set: `search_text=?`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{run.itemID},
		},
		Values: []any{strings.ToLower(strings.Join(searchParts, " "))},
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func boolIncrement(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullablePatchInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableCandidate(value *string) sql.NullString {
	if value == nil || *value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func (repository *ReviewDraftPatches) validCandidateAsset(
	ctx context.Context, transaction *sql.Tx, itemID, assetID string,
) bool {
	valid, err := BindReviewDraftAssets(transaction).ValidCandidate(ctx, itemID, assetID)
	return err == nil && valid
}

func (repository *ReviewDraftPatches) validUploadedAsset(
	ctx context.Context, transaction *sql.Tx, itemID, assetID string,
) bool {
	valid, err := BindReviewDraftAssets(transaction).ValidUploaded(ctx, itemID, assetID)
	return err == nil && valid
}

var _ application.ReviewDraftPatchRepository = (*ReviewDraftPatches)(nil)
