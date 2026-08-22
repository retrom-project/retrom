package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/cleanup"
	"retrom/internal/tagging"

	"github.com/google/uuid"
)

type MetadataPatch struct {
	Title       *string               `json:"title,omitempty"`
	Description *string               `json:"description,omitempty"`
	Developer   *string               `json:"developer,omitempty"`
	Publisher   *string               `json:"publisher,omitempty"`
	Genre       *string               `json:"genre,omitempty"`
	Players     optionalNullableInt64 `json:"players,omitempty"`
	ReleaseYear optionalNullableInt64 `json:"releaseYear,omitempty"`
}

type optionalNullableInt64 struct {
	present bool
	value   *int64
}

func (value *optionalNullableInt64) UnmarshalJSON(contents []byte) error {
	value.present = true
	if string(contents) == "null" {
		value.value = nil
		return nil
	}
	var decoded int64
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	value.value = &decoded
	return nil
}

type optionalNullableString struct {
	present bool
	value   *string
}

func (value *optionalNullableString) UnmarshalJSON(contents []byte) error {
	value.present = true
	if string(contents) == "null" {
		value.value = nil
		return nil
	}
	var decoded string
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	value.value = &decoded
	return nil
}

type SelectedAssets struct {
	CoverCandidateAssetID       *string  `json:"coverCandidateAssetId,omitempty"`
	CoverUploadedAssetID        *string  `json:"coverUploadedAssetId,omitempty"`
	BackgroundCandidateAssetID  *string  `json:"backgroundCandidateAssetId,omitempty"`
	ScreenshotCandidateAssetIDs []string `json:"screenshotCandidateAssetIds,omitempty"`
}

type DraftPatch struct {
	TargetPlatformInstanceID *string                `json:"targetPlatformInstanceId,omitempty"`
	Metadata                 *MetadataPatch         `json:"metadata,omitempty"`
	SelectedValidationID     *string                `json:"selectedValidationId,omitempty"`
	SelectedCandidateID      optionalNullableString `json:"selectedCandidateId,omitempty"`
	SelectedAssets           *SelectedAssets        `json:"selectedAssets,omitempty"`
	DefaultDOSEntry          optionalNullableString `json:"defaultDosEntry,omitempty"`
	TagIDs                   []string               `json:"tagIds"`
}

type DraftResult struct {
	ItemID      string              `json:"itemId"`
	Version     int64               `json:"version"`
	Metadata    map[string]any      `json:"metadata"`
	Tags        []tagging.Reference `json:"tags"`
	UpdatedAtMS int64               `json:"updatedAtMs"`
}

func validField(value string, maximum int, multiline bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) &&
			(!multiline || (character != '\n' && character != '\r' && character != '\t')) {
			return false
		}
	}
	return true
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) PatchDraft(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	patch DraftPatch,
) (DraftResult, error) {
	if invalidDraftPatch(patch) {
		return DraftResult{}, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	run := draftPatchRun{
		service: service, ctx: ctx, transaction: transaction,
		itemID: itemID, expectedVersion: expectedVersion, patch: patch,
	}
	if err := run.load(); err != nil {
		return DraftResult{}, err
	}
	if err := run.applyChanges(); err != nil {
		return DraftResult{}, err
	}
	return run.persist()
}

type draftPatchRun struct {
	service             *Service
	ctx                 context.Context
	transaction         *sql.Tx
	itemID              string
	expectedVersion     int64
	patch               DraftPatch
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
	beforeTags          []tagging.Reference
	targetOrDOSChanged  bool
}

func invalidDraftPatch(patch DraftPatch) bool {
	noChange := patch.TargetPlatformInstanceID == nil && patch.Metadata == nil &&
		patch.SelectedValidationID == nil && !patch.SelectedCandidateID.present &&
		patch.SelectedAssets == nil && !patch.DefaultDOSEntry.present && len(patch.TagIDs) == 0
	return patch.TagIDs == nil || noChange
}

func (run *draftPatchRun) load() error {
	var currentVersion int64
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT d.id,d.target_platform_instance_id,COALESCE(d.selected_validation_id,''),
  d.effective_source_snapshot_id,d.selected_candidate_id,d.cover_candidate_asset_id,
  d.cover_uploaded_asset_id,d.background_candidate_asset_id,d.default_dos_entry,
  d.metadata_json,d.version
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.id=? AND i.state='REVIEW_PENDING'
`, run.itemID).Scan(
		&run.draftID, &run.targetID, &run.validationID, &run.effectiveSnapshotID,
		&run.candidateID, &run.coverID, &run.uploadedCoverID, &run.backgroundID,
		&run.dosEntry, &run.metadataJSON, &currentVersion,
	)
	if err != nil {
		return ErrInvalid
	}
	if currentVersion != run.expectedVersion {
		return ErrVersionConflict
	}
	beforeTags, err := run.service.tags.ReviewDraftReferences(run.ctx, run.transaction, run.draftID)
	if err != nil {
		return fmt.Errorf("libraryimport/review: read draft tags: %w", err)
	}
	run.beforeTags = beforeTags
	if err := json.Unmarshal([]byte(run.metadataJSON), &run.metadata); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func (run *draftPatchRun) applyChanges() error {
	steps := []func() error{
		run.applyMetadata, run.applyTarget, run.applySelectedValidation,
		run.applySelectedCandidate, run.applyDefaultDOSEntry, run.refreshValidation,
		run.applySelectedAssets,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	if run.targetOrDOSChanged && run.validationID == "" {
		return ErrInvalid
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
		if !validField(*patch.Title, 200, false) || *patch.Title == "" {
			return nil, ErrInvalid
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
		if field.value != nil && !validField(*field.value, field.maximum, field.multiline) {
			return nil, ErrInvalid
		}
		if field.value != nil {
			updates[field.key] = *field.value
		}
	}
	if err := validateMetadataNumbers(run.service, patch, updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func validateMetadataNumbers(service *Service, patch MetadataPatch, updates map[string]any) error {
	if patch.Players.present {
		if patch.Players.value != nil && (*patch.Players.value < 1 || *patch.Players.value > 64) {
			return ErrInvalid
		}
		updates["players"] = nullablePatchInt(patch.Players.value)
	}
	if patch.ReleaseYear.present {
		maximumYear := int64(service.now().UTC().Year() + 1)
		if patch.ReleaseYear.value != nil &&
			(*patch.ReleaseYear.value < 1950 || *patch.ReleaseYear.value > maximumYear) {
			return ErrInvalid
		}
		updates["releaseYear"] = nullablePatchInt(patch.ReleaseYear.value)
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
		return ErrInvalid
	}
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT platform_id FROM platform_instances
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL
`, *run.patch.TargetPlatformInstanceID).Scan(&targetPlatform); err != nil {
		return ErrInvalid
	}
	if currentPlatform != targetPlatform {
		return ErrReimportRequiredPlatformChange
	}
	run.targetOrDOSChanged = run.targetID != *run.patch.TargetPlatformInstanceID
	run.targetID = *run.patch.TargetPlatformInstanceID
	return nil
}

func (run *draftPatchRun) applySelectedValidation() error {
	if run.patch.SelectedValidationID == nil {
		return nil
	}
	var targetID, snapshotID, status string
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT target_platform_instance_id,source_snapshot_id,status
FROM import_item_core_validations
WHERE id=? AND import_item_id=?
`, *run.patch.SelectedValidationID, run.itemID).Scan(&targetID, &snapshotID, &status)
	if err != nil || targetID != run.targetID || snapshotID != run.effectiveSnapshotID || status != "READY" {
		return ErrInvalid
	}
	run.validationID = *run.patch.SelectedValidationID
	return nil
}

func (run *draftPatchRun) applySelectedCandidate() error {
	if !run.patch.SelectedCandidateID.present {
		return nil
	}
	if run.patch.SelectedCandidateID.value == nil {
		run.candidateID = sql.NullString{}
		return nil
	}
	var count int
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE c.id=? AND r.import_item_id=? AND r.state='COMPLETED'
`, *run.patch.SelectedCandidateID.value, run.itemID).Scan(&count)
	if err != nil || count != 1 {
		return ErrInvalid
	}
	run.candidateID = sql.NullString{String: *run.patch.SelectedCandidateID.value, Valid: true}
	return nil
}

func (run *draftPatchRun) applyDefaultDOSEntry() error {
	if !run.patch.DefaultDOSEntry.present {
		return nil
	}
	previous := nullable(run.dosEntry)
	if run.patch.DefaultDOSEntry.value == nil {
		run.dosEntry = sql.NullString{}
	} else {
		var count int
		err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*) FROM import_item_dos_entries
WHERE import_item_id=? AND normalized_path=? AND enabled=1
`, run.itemID, *run.patch.DefaultDOSEntry.value).Scan(&count)
		if err != nil || count != 1 {
			return ErrInvalid
		}
		run.dosEntry = sql.NullString{String: *run.patch.DefaultDOSEntry.value, Valid: true}
	}
	run.targetOrDOSChanged = run.targetOrDOSChanged || previous != nullable(run.dosEntry)
	return nil
}

func (run *draftPatchRun) refreshValidation() error {
	if run.patch.SelectedValidationID != nil {
		return nil
	}
	validationID, err := run.service.ensureCompatibleDraftValidation(
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
		return ErrInvalid
	}
	if run.uploadedCoverID.Valid &&
		!run.service.validUploadedAsset(run.ctx, run.transaction, run.itemID, run.uploadedCoverID.String) {
		return ErrInvalid
	}
	for _, assetID := range []sql.NullString{run.coverID, run.backgroundID} {
		if assetID.Valid &&
			!run.service.validCandidateAsset(run.ctx, run.transaction, run.itemID, assetID.String) {
			return ErrInvalid
		}
	}
	return run.replaceScreenshots(assets.ScreenshotCandidateAssetIDs)
}

func (run *draftPatchRun) validateSelectedAssets(assets SelectedAssets) error {
	if len(assets.ScreenshotCandidateAssetIDs) > 32 {
		return ErrInvalid
	}
	selected := make(map[string]struct{}, len(assets.ScreenshotCandidateAssetIDs))
	for _, assetID := range assets.ScreenshotCandidateAssetIDs {
		_, duplicate := selected[assetID]
		if duplicate || !run.service.validCandidateAsset(run.ctx, run.transaction, run.itemID, assetID) {
			return ErrInvalid
		}
		selected[assetID] = struct{}{}
	}
	return nil
}

func (run *draftPatchRun) replaceScreenshots(assetIDs []string) error {
	_, err := run.transaction.ExecContext(run.ctx, `
DELETE FROM review_draft_screenshot_assets
WHERE review_draft_id=(SELECT id FROM review_drafts WHERE import_item_id=?)
`, run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	for ordinal, assetID := range assetIDs {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_draft_screenshot_assets(
  review_draft_id,ordinal,candidate_asset_id,created_at_ms
)
SELECT id,?,?,? FROM review_drafts WHERE import_item_id=?
`, ordinal, assetID, run.service.now().UnixMilli(), run.itemID)
		if err != nil {
			return fmt.Errorf("libraryimport/review: %w", err)
		}
	}
	return nil
}

func (run *draftPatchRun) persist() (DraftResult, error) {
	encoded, err := json.Marshal(run.metadata)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	searchParts, err := run.searchParts()
	if err != nil {
		return DraftResult{}, err
	}
	now := run.service.now().UnixMilli()
	actor := reviewActor(run.ctx)
	actorUserID, _ := actor.UserID.(string)
	_, afterTags, err := run.service.tags.ReplaceReviewDraftTags(
		run.ctx, run.transaction, run.draftID, run.patch.TagIDs, actorUserID, now,
	)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: replace draft tags: %w", err)
	}
	if err := run.updateDraft(encoded, searchParts, now); err != nil {
		return DraftResult{}, err
	}
	if err := run.insertSavedEvent(actor.Kind, actor.UserID, actor.Label, afterTags, now); err != nil {
		return DraftResult{}, err
	}
	if err := run.transaction.Commit(); err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return DraftResult{
		ItemID: run.itemID, Version: run.expectedVersion + 1,
		Metadata: run.metadata, Tags: afterTags, UpdatedAtMS: now,
	}, nil
}

func (run *draftPatchRun) searchParts() ([]string, error) {
	parts := []string{run.itemID}
	rows, err := run.transaction.QueryContext(run.ctx, `
SELECT u.relative_path
FROM import_item_source_snapshot_files s
JOIN upload_files u ON u.id=s.upload_file_id
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
	result, err := run.transaction.ExecContext(run.ctx, `
UPDATE review_drafts
SET target_platform_instance_id=?,selected_validation_id=NULLIF(?,''),
  selected_candidate_id=?,cover_candidate_asset_id=?,cover_uploaded_asset_id=?,
  background_candidate_asset_id=?,default_dos_entry=?,metadata_json=?,
  version=version+1,updated_at_ms=?
WHERE import_item_id=? AND version=?
`, run.targetID, run.validationID, nullable(run.candidateID), nullable(run.coverID),
		nullable(run.uploadedCoverID), nullable(run.backgroundID), nullable(run.dosEntry),
		string(encoded), now, run.itemID, run.expectedVersion)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrVersionConflict
	}
	_, err = run.transaction.ExecContext(run.ctx, `
UPDATE import_items SET search_text=? WHERE id=?
`, strings.ToLower(strings.Join(searchParts, " ")), run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func (run *draftPatchRun) insertSavedEvent(
	actorKind string,
	actorUserID any,
	actorLabel any,
	afterTags []tagging.Reference,
	now int64,
) error {
	eventID, _ := uuid.NewV7()
	beforeJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "metadata": json.RawMessage(run.metadataJSON), "tags": run.beforeTags,
	})
	afterJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "metadata": run.metadata, "tags": afterTags,
	})
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
  after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms
) VALUES(?,?,'DRAFT_SAVED',?,?,?,?,?,?,'{}','{}','{}',?)
`, eventID.String(), run.itemID, actorKind, actorUserID, actorLabel,
		string(beforeJSON), string(afterJSON), string(afterJSON), now)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}
