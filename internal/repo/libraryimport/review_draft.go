package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	tagpersistence "retrom/internal/repo/tagging"

	"github.com/google/uuid"
)

type ReviewDraftPatches struct {
	database *sql.DB
}

func NewReviewDraftPatches(database *sql.DB) *ReviewDraftPatches {
	return &ReviewDraftPatches{database: database}
}

// LoadPatchSnapshot reads all facts used by the application planner from one
// consistent view. It intentionally returns values, not a transaction or
// executor.
func (repository *ReviewDraftPatches) LoadPatchSnapshot(
	ctx context.Context, query application.ReviewDraftPatchQuery,
) (application.ReviewDraftPatchSnapshot, error) {
	if repository == nil || repository.database == nil {
		return application.ReviewDraftPatchSnapshot{}, application.ErrInvalid
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.ReviewDraftPatchSnapshot{}, fmt.Errorf("begin review draft snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)
	return loadPatchSnapshot(ctx, transaction, query)
}

func loadPatchSnapshot(
	ctx context.Context, transaction *sql.Tx, query application.ReviewDraftPatchQuery,
) (application.ReviewDraftPatchSnapshot, error) {
	result, currentVersion, metadataJSON, err := readPatchDraft(ctx, transaction, query.ItemID)
	if err != nil {
		return application.ReviewDraftPatchSnapshot{}, err
	}
	if currentVersion != query.ExpectedVersion {
		return application.ReviewDraftPatchSnapshot{}, application.ErrVersionConflict
	}
	result.ItemID, result.Version = query.ItemID, currentVersion
	if err := json.Unmarshal([]byte(metadataJSON), &result.Metadata); err != nil {
		return application.ReviewDraftPatchSnapshot{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	owner := tagging.Owner{Kind: tagging.OwnerReviewDraft, ID: result.DraftID}
	result.BeforeTags, err = tagpersistence.Bind(transaction).Relations.References(ctx, owner)
	if err != nil {
		return application.ReviewDraftPatchSnapshot{}, fmt.Errorf("libraryimport/review: read draft tags: %w", err)
	}
	if len(query.TagIDs) > 0 {
		result.ActiveTags, err = tagpersistence.Bind(transaction).Tags.ActiveReferences(ctx, query.TagIDs)
		if err != nil {
			return application.ReviewDraftPatchSnapshot{}, fmt.Errorf("libraryimport/review: read active tags: %w", err)
		}
	} else {
		result.ActiveTags = []tagging.Reference{}
	}
	result.SourcePaths, err = readPatchSourcePaths(ctx, transaction, result.EffectiveSnapshotID)
	if err != nil {
		return application.ReviewDraftPatchSnapshot{}, err
	}
	result.ScreenshotAssetIDs, err = readPatchScreenshots(ctx, transaction, result.DraftID)
	if err != nil {
		return application.ReviewDraftPatchSnapshot{}, err
	}
	return result, nil
}

func readPatchDraft(
	ctx context.Context, transaction *sql.Tx, itemID string,
) (application.ReviewDraftPatchSnapshot, int64, string, error) {
	var result application.ReviewDraftPatchSnapshot
	var currentVersion int64
	var metadataJSON string
	var candidateID, coverID, uploadedCoverID, backgroundID, dosEntry sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT d.id,d.target_platform_instance_id,COALESCE(d.selected_validation_id,''),
  d.effective_source_snapshot_id,d.selected_candidate_id,d.cover_candidate_asset_id,
  d.cover_uploaded_asset_id,d.background_candidate_asset_id,d.default_dos_entry,
  d.metadata_json,d.version,
  EXISTS(SELECT 1 FROM rpgmaker_review_profiles profile WHERE profile.review_draft_id=d.id)
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.id=? AND i.state='REVIEW_PENDING'
`, itemID).Scan(
		&result.DraftID, &result.TargetID, &result.ValidationID, &result.EffectiveSnapshotID,
		&candidateID, &coverID, &uploadedCoverID, &backgroundID, &dosEntry,
		&metadataJSON, &currentVersion, &result.IsRPG,
	)
	if err != nil {
		return application.ReviewDraftPatchSnapshot{}, 0, "", application.ErrInvalid
	}
	result.CandidateID = nullablePointer(candidateID)
	result.CoverID = nullablePointer(coverID)
	result.UploadedCoverID = nullablePointer(uploadedCoverID)
	result.BackgroundID = nullablePointer(backgroundID)
	result.DOSEntry = nullablePointer(dosEntry)
	return result, currentVersion, metadataJSON, nil
}

func readPatchSourcePaths(ctx context.Context, transaction *sql.Tx, snapshotID string) ([]string, error) {
	return readPatchIDs(ctx, transaction, `
SELECT u.relative_path
FROM import_item_source_snapshot_files s
JOIN upload_files u ON u.id=s.upload_file_id
WHERE s.source_snapshot_id=?
ORDER BY s.sort_order,s.role,s.logical_name
	`, snapshotID, "read source paths")
}

func readPatchScreenshots(ctx context.Context, transaction *sql.Tx, draftID string) ([]string, error) {
	return readPatchIDs(ctx, transaction, `
SELECT candidate_asset_id FROM review_draft_screenshot_assets
WHERE review_draft_id=? ORDER BY ordinal
	`, draftID, "read screenshots")
}

func readPatchIDs(
	ctx context.Context, transaction *sql.Tx, query string, argument string, operation string,
) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, query, argument)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/review: %s: %w", operation, err)
	}
	defer func() { _ = rows.Close() }()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("libraryimport/review: %s: %w", operation, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/review: %s: %w", operation, err)
	}
	return values, nil
}

func (repository *ReviewDraftPatches) CommitPatch(
	ctx context.Context, plan application.ReviewDraftWritePlan,
) (application.DraftResult, error) {
	if repository == nil || repository.database == nil || plan.ItemID == "" || plan.DraftID == "" ||
		plan.ExpectedVersion < 1 || plan.ExpectedTargetID == "" || plan.ExpectedEffectiveSnapshotID == "" ||
		plan.NowMS < 0 {
		return application.DraftResult{}, application.ErrInvalid
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("begin review draft patch: %w", err)
	}
	defer dbexec.Rollback(transaction)
	run := draftPatchRun{ctx: ctx, transaction: transaction, plan: plan}
	if err := run.load(); err != nil {
		return application.DraftResult{}, err
	}
	if err := run.applyPlan(); err != nil {
		return application.DraftResult{}, err
	}
	return run.persist()
}

type draftPatchRun struct {
	ctx         context.Context
	transaction *sql.Tx
	plan        application.ReviewDraftWritePlan

	draftID, targetID, validationID, effectiveSnapshotID string
	metadataJSON                                         string
	currentVersion                                       int64
	currentDOS                                           *string
	isRPG                                                bool
	beforeTags                                           []tagging.Reference
	targetOrDOSChanged                                   bool
}

func (run *draftPatchRun) load() error {
	var candidateID, coverID, uploadedCoverID, backgroundID, dosEntry sql.NullString
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT d.id,d.target_platform_instance_id,COALESCE(d.selected_validation_id,''),
  d.effective_source_snapshot_id,d.selected_candidate_id,d.cover_candidate_asset_id,
  d.cover_uploaded_asset_id,d.background_candidate_asset_id,d.default_dos_entry,
  d.metadata_json,d.version,
  EXISTS(SELECT 1 FROM rpgmaker_review_profiles profile WHERE profile.review_draft_id=d.id)
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.id=? AND i.state='REVIEW_PENDING'
`, run.plan.ItemID).Scan(
		&run.draftID, &run.targetID, &run.validationID, &run.effectiveSnapshotID,
		&candidateID, &coverID, &uploadedCoverID, &backgroundID, &dosEntry,
		&run.metadataJSON, &run.currentVersion, &run.isRPG,
	)
	if err != nil {
		return application.ErrInvalid
	}
	run.currentDOS = nullablePointer(dosEntry)
	if run.draftID != run.plan.DraftID || run.currentVersion != run.plan.ExpectedVersion {
		if run.currentVersion != run.plan.ExpectedVersion {
			return application.ErrVersionConflict
		}
		return application.ErrInvalid
	}
	if run.targetID != run.plan.ExpectedTargetID || run.validationID != run.plan.ExpectedValidationID ||
		run.effectiveSnapshotID != run.plan.ExpectedEffectiveSnapshotID ||
		run.isRPG != run.plan.ExpectedIsRPG || !sameNullable(run.currentDOS, run.plan.ExpectedDOSEntry) {
		return application.ErrVersionConflict
	}
	run.beforeTags, err = tagpersistence.Bind(run.transaction).Relations.References(
		run.ctx, tagging.Owner{Kind: tagging.OwnerReviewDraft, ID: run.draftID},
	)
	if err != nil {
		return fmt.Errorf("libraryimport/review: read draft tags: %w", err)
	}
	if !tagging.ReferencesEqual(run.beforeTags, run.plan.Tags.Before) {
		return application.ErrVersionConflict
	}
	return nil
}

func (run *draftPatchRun) applyPlan() error {
	if run.plan.TargetID != run.targetID {
		if err := run.validateTarget(run.plan.TargetID); err != nil {
			return err
		}
		run.targetOrDOSChanged = true
	}
	currentDOS := run.currentDOSEntry()
	if !sameNullable(currentDOS, run.plan.DOSEntry) {
		if run.plan.DOSEntry != nil && !run.validDOSEntry(*run.plan.DOSEntry) {
			return application.ErrInvalid
		}
		run.targetOrDOSChanged = true
	}
	if err := run.applyValidation(); err != nil {
		return err
	}
	if err := run.validateCandidate(); err != nil {
		return err
	}
	if err := run.applyRPGBinding(); err != nil {
		return err
	}
	if err := run.applyAssets(); err != nil {
		return err
	}
	if run.targetOrDOSChanged && run.plan.ValidationID == "" {
		return application.ErrInvalid
	}
	return nil
}

func (run *draftPatchRun) validateTarget(targetID string) error {
	var currentPlatform, targetPlatform string
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT platform_id FROM platform_instances WHERE id=?
`, run.targetID).Scan(&currentPlatform); err != nil {
		return application.ErrInvalid
	}
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT platform_id FROM platform_instances
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL
`, targetID).Scan(&targetPlatform); err != nil {
		return application.ErrInvalid
	}
	if currentPlatform != targetPlatform {
		return application.ErrReimportRequiredPlatformChange
	}
	run.targetID = targetID
	return nil
}

func (run *draftPatchRun) currentDOSEntry() *string {
	if run.currentDOS == nil {
		return nil
	}
	value := *run.currentDOS
	return &value
}

func (run *draftPatchRun) validDOSEntry(value string) bool {
	var count int
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*) FROM import_item_dos_entries
WHERE import_item_id=? AND normalized_path=? AND enabled=1
`, run.plan.ItemID, value).Scan(&count)
	return err == nil && count == 1
}

func (run *draftPatchRun) applyValidation() error {
	if err := run.createValidation(); err != nil {
		return err
	}
	return run.selectValidation()
}

func (run *draftPatchRun) createValidation() error {
	create := run.plan.ValidationCreate
	if create == nil {
		if run.plan.ValidationCopy != nil {
			return application.ErrInvalid
		}
		return nil
	}
	if create.ID == "" || create.ItemID != run.plan.ItemID || create.TargetPlatformInstanceID != run.targetID ||
		create.SourceSnapshotID != run.effectiveSnapshotID || !sameNullable(create.DefaultDOSEntry, run.plan.DOSEntry) ||
		(create.Status == "READY") != (run.plan.ValidationID == create.ID) {
		return application.ErrInvalid
	}
	if err := BindReviewValidation(run.transaction).Create(run.ctx, *create); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	copyFiles := run.plan.ValidationCopy
	if copyFiles == nil || copyFiles.ValidationID != create.ID {
		return application.ErrInvalid
	}
	if err := BindReviewValidation(run.transaction).CopyFiles(run.ctx, *copyFiles); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func (run *draftPatchRun) selectValidation() error {
	if run.plan.ValidationID == "" {
		run.validationID = ""
		return nil
	}
	if run.isRPG {
		if run.plan.ValidationCreate != nil &&
			run.plan.ValidationCreate.ID == run.plan.ValidationID &&
			run.plan.ValidationCreate.CoreID == "rpgmaker" {
			run.validationID = run.plan.ValidationID
			return nil
		}
		if run.plan.ValidationCreate == nil && run.plan.ValidationID == run.validationID {
			return nil
		}
		return application.ErrInvalid
	}
	var targetID, snapshotID, status string
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT target_platform_instance_id,source_snapshot_id,status
FROM import_item_core_validations
WHERE id=? AND import_item_id=?
`, run.plan.ValidationID, run.plan.ItemID).Scan(&targetID, &snapshotID, &status)
	if err != nil || targetID != run.targetID || snapshotID != run.effectiveSnapshotID || status != "READY" {
		return application.ErrInvalid
	}
	run.validationID = run.plan.ValidationID
	return nil
}

func (run *draftPatchRun) validateCandidate() error {
	if run.plan.CandidateID == nil {
		return nil
	}
	var count int
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE c.id=? AND r.import_item_id=? AND r.state='COMPLETED'
	`, *run.plan.CandidateID, run.plan.ItemID).Scan(&count)
	if err != nil || count != 1 {
		return application.ErrInvalid
	}
	return nil
}

func (run *draftPatchRun) applyRPGBinding() error {
	if run.plan.RPGSelfContainedOverride == nil {
		return nil
	}
	if !run.isRPG || run.targetOrDOSChanged {
		return application.ErrInvalid
	}
	var generation string
	if err := run.transaction.QueryRowContext(
		run.ctx,
		`SELECT generation FROM rpgmaker_review_profiles WHERE review_draft_id=?`,
		run.draftID,
	).Scan(&generation); err != nil {
		return application.ErrInvalid
	}
	if *run.plan.RPGSelfContainedOverride && (generation == "RPGMV" || generation == "RPGMZ") {
		return application.ErrInvalid
	}
	if _, err := run.transaction.ExecContext(run.ctx, `
UPDATE rpgmaker_review_profiles SET self_contained_override=?,updated_at_ms=? WHERE review_draft_id=?
`, boolNumber(*run.plan.RPGSelfContainedOverride), run.plan.NowMS, run.draftID); err != nil {
		return fmt.Errorf("libraryimport/review self-contained confirmation: %w", err)
	}
	if run.plan.RPGDependencyDigest != "" {
		if _, err := run.transaction.ExecContext(run.ctx, `
UPDATE rpgmaker_review_profiles SET dependency_snapshot_sha256=?,updated_at_ms=? WHERE review_draft_id=?
`, run.plan.RPGDependencyDigest, run.plan.NowMS, run.draftID); err != nil {
			return fmt.Errorf("libraryimport/RPG dependency digest: %w", err)
		}
	}
	return nil
}

func (run *draftPatchRun) applyAssets() error {
	if !run.plan.AssetsChanged {
		return nil
	}
	if err := run.validateAssets(); err != nil {
		return err
	}
	return run.replaceScreenshotAssets()
}

func (run *draftPatchRun) validateAssets() error {
	if len(run.plan.ScreenshotAssetIDs) > 32 {
		return application.ErrInvalid
	}
	selected := make(map[string]struct{}, len(run.plan.ScreenshotAssetIDs))
	for _, assetID := range run.plan.ScreenshotAssetIDs {
		if _, duplicate := selected[assetID]; duplicate || !run.validCandidateAsset(assetID) {
			return application.ErrInvalid
		}
		selected[assetID] = struct{}{}
	}
	if run.plan.CoverID != nil && run.plan.UploadedCoverID != nil {
		return application.ErrInvalid
	}
	if run.plan.UploadedCoverID != nil && !run.validUploadedAsset(*run.plan.UploadedCoverID) {
		return application.ErrInvalid
	}
	for _, assetID := range []*string{run.plan.CoverID, run.plan.BackgroundID} {
		if assetID != nil && !run.validCandidateAsset(*assetID) {
			return application.ErrInvalid
		}
	}
	return nil
}

func (run *draftPatchRun) replaceScreenshotAssets() error {
	if _, err := run.transaction.ExecContext(run.ctx, `
DELETE FROM review_draft_screenshot_assets WHERE review_draft_id=?
`, run.draftID); err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	for ordinal, assetID := range run.plan.ScreenshotAssetIDs {
		if _, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_draft_screenshot_assets(review_draft_id,ordinal,candidate_asset_id,created_at_ms)
VALUES(?,?,?,?)
`, run.draftID, ordinal, assetID, run.plan.NowMS); err != nil {
			return fmt.Errorf("libraryimport/review: %w", err)
		}
	}
	return nil
}

func (run *draftPatchRun) validCandidateAsset(assetID string) bool {
	var count int
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*)
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=? AND r.import_item_id=? AND r.state='COMPLETED' AND a.status='READY'
`, assetID, run.plan.ItemID).Scan(&count)
	return err == nil && count == 1
}

func (run *draftPatchRun) validUploadedAsset(assetID string) bool {
	var count int
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT count(*) FROM review_uploaded_assets WHERE id=? AND import_item_id=? AND kind='COVER'
`, assetID, run.plan.ItemID).Scan(&count)
	return err == nil && count == 1
}

func (run *draftPatchRun) persist() (application.DraftResult, error) {
	encoded, err := application.EncodeMetadata(run.plan.Metadata)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: encode metadata: %w", err)
	}
	active, err := tagpersistence.Bind(run.transaction).Tags.ActiveReferences(
		run.ctx, tagging.ReferenceIDs(run.plan.Tags.After),
	)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: read active tags: %w", err)
	}
	actualTags, err := tagging.BuildReplacementPlan(
		tagging.Owner{Kind: tagging.OwnerReviewDraft, ID: run.draftID}, run.beforeTags, active,
		run.plan.Tags.ActorUserID, run.plan.NowMS,
	)
	if err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: plan draft tags: %w", err)
	}
	if !tagging.ReferencesEqual(actualTags.After, run.plan.Tags.After) {
		return application.DraftResult{}, application.ErrVersionConflict
	}
	if err := tagpersistence.ApplyReplacementPlan(run.ctx, run.transaction, actualTags); err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: replace draft tags: %w", err)
	}
	if err := run.updateDraft(encoded); err != nil {
		return application.DraftResult{}, err
	}
	if err := run.insertSavedEvent(actualTags.After); err != nil {
		return application.DraftResult{}, err
	}
	if err := run.transaction.Commit(); err != nil {
		return application.DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return application.DraftResult{
		ItemID: run.plan.ItemID, Version: run.plan.ExpectedVersion + 1,
		Metadata: run.plan.Metadata, Tags: actualTags.After, UpdatedAtMS: run.plan.NowMS,
	}, nil
}

func (run *draftPatchRun) updateDraft(encoded []byte) error {
	result, err := recordstore.UpdateReviewDrafts(run.ctx, run.transaction, recordstore.Update{
		Set: `
target_platform_instance_id=?,selected_validation_id=NULLIF(?,''),
  selected_candidate_id=?,cover_candidate_asset_id=?,cover_uploaded_asset_id=?,
  background_candidate_asset_id=?,default_dos_entry=?,metadata_json=?,
  version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `import_item_id=? AND version=?`, Args: []any{run.plan.ItemID, run.plan.ExpectedVersion},
		},
		Values: []any{
			run.targetID, run.validationID, nullableValue(run.plan.CandidateID), nullableValue(run.plan.CoverID),
			nullableValue(run.plan.UploadedCoverID), nullableValue(run.plan.BackgroundID), nullableValue(run.plan.DOSEntry),
			string(encoded), run.plan.NowMS,
		},
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return application.ErrVersionConflict
	}
	_, err = recordstore.UpdateImportItems(run.ctx, run.transaction, recordstore.Update{
		Set: `search_text=?`, Scope: recordstore.Scope{Where: `id=?`, Args: []any{run.plan.ItemID}},
		Values: []any{application.SearchText(run.plan.SearchParts)},
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func (run *draftPatchRun) insertSavedEvent(afterTags []tagging.Reference) error {
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("libraryimport/review: create save event identity: %w", err)
	}
	beforeJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "metadata": json.RawMessage(run.metadataJSON), "tags": run.beforeTags,
	})
	afterJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "metadata": run.plan.Metadata, "tags": afterTags,
	})
	_, err = recordstore.CreateReviewEvents(run.ctx, run.transaction, `
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
  after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms
) VALUES(?,?,'DRAFT_SAVED',?,?,?,?,?,?,?,?,?,?)
`, eventID.String(), run.plan.ItemID, run.plan.ActorKind, nullableValue(run.plan.ActorUserID),
		nullableValue(run.plan.ActorLabel), string(beforeJSON), string(afterJSON),
		`{"schemaVersion":2,"metadataOrTagsChanged":true}`, `{"schemaVersion":2}`,
		`{"schemaVersion":2}`, `{"schemaVersion":2}`, run.plan.NowMS)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func nullablePointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copyValue := value.String
	return &copyValue
}

func nullableValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func sameNullable(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

var _ interface {
	LoadPatchSnapshot(context.Context, application.ReviewDraftPatchQuery) (application.ReviewDraftPatchSnapshot, error)
	CommitPatch(context.Context, application.ReviewDraftWritePlan) (application.DraftResult, error)
} = (*ReviewDraftPatches)(nil)
