package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"

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
}

type DraftResult struct {
	ItemID      string         `json:"itemId"`
	Version     int64          `json:"version"`
	Metadata    map[string]any `json:"metadata"`
	UpdatedAtMS int64          `json:"updatedAtMs"`
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

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) PatchDraft(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	patch DraftPatch,
) (DraftResult, error) {
	if patch.TargetPlatformInstanceID == nil && patch.Metadata == nil && patch.SelectedValidationID == nil &&
		!patch.SelectedCandidateID.present &&
		patch.SelectedAssets == nil &&
		!patch.DefaultDOSEntry.present {
		return DraftResult{}, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var importJobID, targetID, validationID, effectiveSnapshotID, metadataJSON string
	targetOrDOSChanged := false
	var currentVersion int64
	var candidateID, coverID, uploadedCoverID, backgroundID, dosEntry sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT i.import_job_id,
d.target_platform_instance_id,
COALESCE(d.selected_validation_id,
''),
d.effective_source_snapshot_id,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id,
d.default_dos_entry,
d.metadata_json,
d.version
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.id=?
AND i.state='REVIEW_PENDING'
`, itemID).
		Scan(
			&importJobID,
			&targetID,
			&validationID,
			&effectiveSnapshotID,
			&candidateID,
			&coverID,
			&uploadedCoverID,
			&backgroundID,
			&dosEntry,
			&metadataJSON,
			&currentVersion,
		)
	if err != nil || currentVersion != expectedVersion {
		return DraftResult{}, ErrInvalid
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if patch.Metadata != nil {
		updates := map[string]any{}
		if patch.Metadata.Title != nil {
			if !validField(*patch.Metadata.Title, 200, false) || *patch.Metadata.Title == "" {
				return DraftResult{}, ErrInvalid
			}
			updates["title"] = *patch.Metadata.Title
		}
		metadataFields := map[string]*string{
			"description": patch.Metadata.Description,
			"developer":   patch.Metadata.Developer,
			"publisher":   patch.Metadata.Publisher,
			"genre":       patch.Metadata.Genre,
		}
		for key, value := range metadataFields {
			if value == nil {
				continue
			}
			maximum := 200
			multiline := false
			if key == "description" {
				maximum, multiline = 10_000, true
			}
			if !validField(*value, maximum, multiline) {
				return DraftResult{}, ErrInvalid
			}
			updates[key] = *value
		}
		if patch.Metadata.Players.present {
			if patch.Metadata.Players.value != nil &&
				(*patch.Metadata.Players.value < 1 || *patch.Metadata.Players.value > 64) {
				return DraftResult{}, ErrInvalid
			}
			updates["players"] = nullablePatchInt(patch.Metadata.Players.value)
		}
		if patch.Metadata.ReleaseYear.present {
			maximumYear := int64(service.now().UTC().Year() + 1)
			if patch.Metadata.ReleaseYear.value != nil &&
				(*patch.Metadata.ReleaseYear.value < 1950 || *patch.Metadata.ReleaseYear.value > maximumYear) {
				return DraftResult{}, ErrInvalid
			}
			updates["releaseYear"] = nullablePatchInt(patch.Metadata.ReleaseYear.value)
		}
		for key, value := range updates {
			metadata[key] = value
		}
	}
	if patch.TargetPlatformInstanceID != nil {
		var currentPlatform, targetPlatform string
		if err := transaction.QueryRowContext(ctx, `
SELECT platform_id
FROM platform_instances
WHERE id=?
`, targetID).Scan(&currentPlatform); err != nil {
			return DraftResult{}, ErrInvalid
		}
		if err := transaction.QueryRowContext(ctx, `
SELECT platform_id
FROM platform_instances
WHERE id=?
AND enabled=1
AND deleted_at_ms IS NULL
`, *patch.TargetPlatformInstanceID).Scan(&targetPlatform); err != nil {
			return DraftResult{}, ErrInvalid
		}
		if currentPlatform != targetPlatform {
			return DraftResult{}, ErrReimportRequiredPlatformChange
		}
		targetOrDOSChanged = targetID != *patch.TargetPlatformInstanceID
		targetID = *patch.TargetPlatformInstanceID
	}
	if patch.SelectedValidationID != nil {
		var validationTarget, validationSnapshotID, status string
		if err := transaction.QueryRowContext(ctx, `
SELECT target_platform_instance_id,
source_snapshot_id,
status
FROM import_item_core_validations
WHERE id=?
AND import_item_id=?
`, *patch.SelectedValidationID, itemID).Scan(&validationTarget, &validationSnapshotID, &status); err != nil ||
			validationTarget != targetID ||
			validationSnapshotID != effectiveSnapshotID ||
			status != "READY" {
			return DraftResult{}, ErrInvalid
		}
		validationID = *patch.SelectedValidationID
	}
	if patch.SelectedCandidateID.present {
		if patch.SelectedCandidateID.value == nil {
			candidateID = sql.NullString{}
		} else {
			var count int
			if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE c.id=?
AND r.import_item_id=?
AND r.state='COMPLETED'
`, *patch.SelectedCandidateID.value, itemID).Scan(&count); err != nil || count != 1 {
				return DraftResult{}, ErrInvalid
			}
			candidateID = sql.NullString{String: *patch.SelectedCandidateID.value, Valid: true}
		}
	}
	if patch.DefaultDOSEntry.present {
		previousDOSEntry := nullable(dosEntry)
		if patch.DefaultDOSEntry.value == nil {
			dosEntry = sql.NullString{}
		} else {
			var count int
			if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM import_item_dos_entries
WHERE import_item_id=?
AND normalized_path=?
AND enabled=1
`, itemID, *patch.DefaultDOSEntry.value).Scan(&count); err != nil || count != 1 {
				return DraftResult{}, ErrInvalid
			}
			dosEntry = sql.NullString{String: *patch.DefaultDOSEntry.value, Valid: true}
		}
		targetOrDOSChanged = targetOrDOSChanged || previousDOSEntry != nullable(dosEntry)
	}
	if patch.SelectedValidationID == nil {
		refreshedValidationID, refreshErr := service.ensureCompatibleDraftValidation(
			ctx,
			transaction,
			itemID,
			targetID,
			dosEntry,
		)
		if refreshErr != nil {
			return DraftResult{}, refreshErr
		}
		validationID = refreshedValidationID
	}
	if targetOrDOSChanged && validationID == "" {
		return DraftResult{}, ErrInvalid
	}
	if patch.SelectedAssets != nil {
		selected := map[string]struct{}{}
		if len(patch.SelectedAssets.ScreenshotCandidateAssetIDs) > 32 {
			return DraftResult{}, ErrInvalid
		}
		for _, id := range patch.SelectedAssets.ScreenshotCandidateAssetIDs {
			if _, exists := selected[id]; exists || !service.validCandidateAsset(ctx, transaction, itemID, id) {
				return DraftResult{}, ErrInvalid
			}
			selected[id] = struct{}{}
		}
		coverID = nullableCandidate(patch.SelectedAssets.CoverCandidateAssetID)
		uploadedCoverID = nullableCandidate(patch.SelectedAssets.CoverUploadedAssetID)
		if coverID.Valid && uploadedCoverID.Valid {
			return DraftResult{}, ErrInvalid
		}
		if uploadedCoverID.Valid && !service.validUploadedAsset(ctx, transaction, itemID, uploadedCoverID.String) {
			return DraftResult{}, ErrInvalid
		}
		backgroundID = nullableCandidate(patch.SelectedAssets.BackgroundCandidateAssetID)
		for _, id := range []sql.NullString{coverID, backgroundID} {
			if id.Valid && !service.validCandidateAsset(ctx, transaction, itemID, id.String) {
				return DraftResult{}, ErrInvalid
			}
		}
		if _, err := transaction.ExecContext(ctx, `
DELETE
FROM review_draft_screenshot_assets
WHERE review_draft_id=(SELECT id
FROM review_drafts
WHERE import_item_id=?)
`, itemID); err != nil {
			return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
		}
		for ordinal, id := range patch.SelectedAssets.ScreenshotCandidateAssetIDs {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_draft_screenshot_assets(review_draft_id,
ordinal,
candidate_asset_id,
created_at_ms) SELECT id,
?,
?,
?
FROM review_drafts
WHERE import_item_id=?
`, ordinal, id, service.now().UnixMilli(), itemID); err != nil {
				return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
			}
		}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	searchParts := []string{itemID}
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT u.relative_path
FROM import_item_source_snapshot_files s
JOIN upload_files u ON u.id=s.upload_file_id
WHERE s.source_snapshot_id=?
ORDER BY s.sort_order,
s.role,
s.logical_name
`,
		itemID,
	)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
		}
		searchParts = append(searchParts, path)
	}
	if err := rows.Err(); err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if title, ok := metadata["title"].(string); ok {
		searchParts = append(searchParts, title)
	}
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(
		ctx,
		`
UPDATE review_drafts
SET target_platform_instance_id=?,
selected_validation_id=NULLIF(?,
''),
selected_candidate_id=?,
cover_candidate_asset_id=?,
cover_uploaded_asset_id=?,
background_candidate_asset_id=?,
default_dos_entry=?,
metadata_json=?,
version=version+1,
updated_at_ms=?
WHERE import_item_id=?
AND version=?
`,
		targetID,
		validationID,
		nullable(candidateID),
		nullable(coverID),
		nullable(uploadedCoverID),
		nullable(backgroundID),
		nullable(dosEntry),
		string(encoded),
		now,
		itemID,
		expectedVersion,
	)
	if err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return DraftResult{}, ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET search_text=?
WHERE id=?
`, strings.ToLower(strings.Join(searchParts, " ")), itemID); err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	eventID, _ := uuid.NewV7()
	actor := reviewActor(ctx)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,
import_item_id,
event_type,
actor_kind,
actor_user_id,
actor_label,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
created_at_ms) VALUES(?,
?,
'DRAFT_SAVED',
?,
?,
?,
?,
?,
?,
'{}',
'{}',
'{}',
?)
`,
		eventID.String(), itemID, actor.Kind, actor.UserID, actor.Label,
		metadataJSON, string(encoded), string(encoded), now,
	); err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return DraftResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return DraftResult{ItemID: itemID, Version: expectedVersion + 1, Metadata: metadata, UpdatedAtMS: now}, nil
}

//nolint:funlen,gocognit,gocyclo,nestif // Keep immutable validation refresh branches together for auditability.
func (service *Service) ensureCompatibleDraftValidation(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, targetID string,
	dosEntry sql.NullString,
) (string, error) {
	var effectiveSnapshotID, effectiveManifestDigest string
	if err := transaction.QueryRowContext(ctx, `
SELECT snapshot.id,snapshot.source_manifest_digest
FROM review_drafts draft
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE draft.import_item_id=?
`, itemID).Scan(&effectiveSnapshotID, &effectiveManifestDigest); err != nil {
		return "", ErrInvalid
	}
	var platformVersion int64
	var coreID, artifactID string
	var datID sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT p.version,
p.default_core_id,
a.id,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1)
FROM platform_instances p
JOIN core_artifacts a ON a.core_id=p.default_core_id
AND a.enabled=1
WHERE p.id=?
AND p.enabled=1
AND p.deleted_at_ms IS NULL
`, targetID).Scan(&platformVersion, &coreID, &artifactID, &datID); err != nil {
		return "", ErrInvalid
	}
	var sourceID, sourceManifestDigest, sourceStatus, compatibilityCode, dependencySnapshot string
	var exactBIOSState draftBIOSState
	err := transaction.QueryRowContext(ctx, `
SELECT id,
source_manifest_digest,
status,
compatibility_code,
dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=?
AND source_snapshot_id=?
AND target_platform_instance_id=?
AND platform_instance_version=?
AND core_id=?
AND core_artifact_id=?
AND dat_version_id IS ?
AND default_dos_entry IS ?
ORDER BY created_at_ms DESC,
id DESC LIMIT 1
`, itemID, effectiveSnapshotID, targetID, platformVersion, coreID, artifactID, nullable(datID), nullable(dosEntry)).
		Scan(&sourceID, &sourceManifestDigest, &sourceStatus, &compatibilityCode, &dependencySnapshot)
	if err == nil {
		biosState, stateErr := resolveDraftBIOSState(
			ctx, transaction, effectiveSnapshotID, artifactID, dependencySnapshot, sourceStatus, compatibilityCode,
		)
		if stateErr != nil {
			return "", stateErr
		}
		exactBIOSState = biosState
		if !biosState.tracked && sourceStatus == "READY" {
			return sourceID, nil
		}
		if biosState.tracked && biosState.snapshotJSON == dependencySnapshot &&
			biosState.status == sourceStatus && biosState.code == compatibilityCode {
			if sourceStatus == "READY" {
				return sourceID, nil
			}
			return "", nil
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	if sourceID == "" || sourceStatus != "READY" && !exactBIOSState.tracked {
		err = transaction.QueryRowContext(ctx, `
SELECT id,
source_manifest_digest,
status,
compatibility_code,
dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=?
AND source_snapshot_id=?
AND core_id=?
AND core_artifact_id=?
AND dat_version_id IS ?
AND status='READY'
ORDER BY created_at_ms DESC,
id DESC LIMIT 1
`, itemID, effectiveSnapshotID, coreID, artifactID, nullable(datID)).
			Scan(&sourceID, &sourceManifestDigest, &sourceStatus, &compatibilityCode, &dependencySnapshot)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("libraryimport/review: %w", err)
		}
	}
	biosState, err := resolveDraftBIOSState(
		ctx, transaction, effectiveSnapshotID, artifactID, dependencySnapshot, sourceStatus, compatibilityCode,
	)
	if err != nil {
		return "", err
	}
	if biosState.tracked {
		sourceStatus = biosState.status
		compatibilityCode = biosState.code
		dependencySnapshot = biosState.snapshotJSON
	}
	input, _ := json.Marshal(
		map[string]any{
			"schemaVersion":            1,
			"validatorVersion":         "review-compatible-v3",
			"sourceSnapshotId":         effectiveSnapshotID,
			"sourceManifestDigest":     effectiveManifestDigest,
			"targetPlatformInstanceId": targetID,
			"platformInstanceVersion":  platformVersion,
			"coreArtifactId":           artifactID,
			"datVersionId":             nullable(datID),
			"defaultDosEntry":          nullable(dosEntry),
			"dependencySnapshot":       dependencySnapshot,
			"status":                   sourceStatus,
		},
	)
	digest := sha256.Sum256(input)
	createdID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,
import_item_id,
target_platform_instance_id,
platform_instance_version,
core_id,
core_artifact_id,
dat_version_id,
default_dos_entry,
source_manifest_digest,
source_snapshot_id,
prepublish_input_digest,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		createdID.String(),
		itemID,
		targetID,
		platformVersion,
		coreID,
		artifactID,
		nullable(datID),
		nullable(dosEntry),
		effectiveManifestDigest,
		effectiveSnapshotID,
		hex.EncodeToString(digest[:]),
		sourceStatus,
		compatibilityCode,
		dependencySnapshot,
		now,
	); err != nil {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,
role,
logical_name,
blob_id,
sort_order,
created_at_ms) SELECT ?,
role,
logical_name,
blob_id,
sort_order,
?
FROM import_item_validation_files
WHERE import_item_core_validation_id=?
AND (?=0 OR role<>'BIOS_BUNDLE')
	`, createdID.String(), now, sourceID, biosState.replaceBundle); err != nil {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	if biosState.tracked {
		var sortOrder int
		if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(sort_order),-1)+1
FROM import_item_validation_files
WHERE import_item_core_validation_id=?
`, createdID.String()).Scan(&sortOrder); err != nil {
			return "", fmt.Errorf("libraryimport/review: %w", err)
		}
		for _, dependency := range biosState.dependencies {
			if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
				continue
			}
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,
role,
logical_name,
blob_id,
sort_order,
created_at_ms) VALUES(?,'BIOS_BUNDLE',?,?,?,?)
`, createdID.String(), dependency.LogicalName, *dependency.BlobID, sortOrder, now); err != nil {
				return "", fmt.Errorf("libraryimport/review: %w", err)
			}
			sortOrder++
		}
	}
	if sourceStatus != "READY" {
		return "", nil
	}
	return createdID.String(), nil
}

type draftBIOSState struct {
	tracked       bool
	replaceBundle bool
	snapshotJSON  string
	status        string
	code          string
	dependencies  []corevalidation.BIOSDependency
}

func resolveDraftBIOSState(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID, artifactID, previousSnapshot, previousStatus, previousCode string,
) (draftBIOSState, error) {
	if !isStaticBIOSSnapshot(previousSnapshot) {
		return resolveArcadeDraftBIOSState(
			ctx, transaction, artifactID, previousSnapshot, previousStatus, previousCode,
		)
	}
	var logicalName string
	err := transaction.QueryRowContext(ctx, `
SELECT logical_name
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='CONTENT'
ORDER BY sort_order,logical_name
LIMIT 1
`, sourceSnapshotID).Scan(&logicalName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return draftBIOSState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	snapshot, status, code, err := corevalidation.ResolveBIOS(ctx, transaction, artifactID, logicalName)
	if err != nil {
		return draftBIOSState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return draftBIOSState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return draftBIOSState{
		tracked:       true,
		replaceBundle: true,
		snapshotJSON:  string(encoded),
		status:        status,
		code:          code,
		dependencies:  snapshot.BIOS,
	}, nil
}

type arcadeDraftDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type arcadeDraftSnapshot struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	Machine           string                  `json:"machine"`
	DatVersionID      string                  `json:"datVersionId"`
	Closure           json.RawMessage         `json:"closure"`
	Dependencies      []arcadeDraftDependency `json:"dependencies"`
	MissingEntries    []string                `json:"missingEntries"`
	MismatchedEntries []string                `json:"mismatchedEntries"`
	Warnings          []string                `json:"warnings"`
}

func resolveArcadeDraftBIOSState(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, previousSnapshot, previousStatus, previousCode string,
) (draftBIOSState, error) {
	snapshot, valid := parseArcadeDraftSnapshot(previousSnapshot)
	if !valid {
		return draftBIOSState{}, nil
	}
	state := draftBIOSState{
		tracked: true, replaceBundle: false, snapshotJSON: previousSnapshot, status: previousStatus, code: previousCode,
		dependencies: make([]corevalidation.BIOSDependency, 0),
	}
	resolvedNames := make(map[string]struct{})
	for index := range snapshot.Dependencies {
		dependency := &snapshot.Dependencies[index]
		if dependency.Kind != "BIOS_OR_BASE" || dependency.State != "MISSING" {
			continue
		}
		resolved, dependencyState, err := resolveArcadeBIOSDependency(
			ctx, transaction, artifactID, dependency.Machine+".zip",
		)
		if err != nil {
			return draftBIOSState{}, err
		}
		if resolved == nil {
			continue
		}
		dependency.State = dependencyState
		if dependencyState == "HASH_WARNING" {
			snapshot.Warnings = append(snapshot.Warnings, dependency.Machine+".zip:HASH_WARNING")
		}
		resolvedNames[dependency.Machine+".zip"] = struct{}{}
		state.dependencies = append(state.dependencies, *resolved)
	}
	if len(resolvedNames) == 0 {
		return state, nil
	}
	missing := snapshot.MissingEntries[:0]
	for _, entry := range snapshot.MissingEntries {
		if _, resolved := resolvedNames[entry]; !resolved {
			missing = append(missing, entry)
		}
	}
	snapshot.MissingEntries = missing
	sort.Strings(snapshot.Warnings)
	if previousCode == "LAUNCH_BIOS_MISSING" && len(snapshot.MissingEntries) == 0 {
		state.status, state.code = "READY", "READY"
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return draftBIOSState{}, fmt.Errorf("libraryimport/review: encode arcade BIOS snapshot: %w", err)
	}
	state.snapshotJSON = string(encoded)
	return state, nil
}

func parseArcadeDraftSnapshot(raw string) (arcadeDraftSnapshot, bool) {
	var snapshot arcadeDraftSnapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil ||
		(snapshot.SchemaVersion != 1 && snapshot.SchemaVersion != 2) || snapshot.Machine == "" ||
		snapshot.DatVersionID == "" || snapshot.Dependencies == nil {
		return arcadeDraftSnapshot{}, false
	}
	return snapshot, true
}

func resolveArcadeBIOSDependency(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, logicalName string,
) (*corevalidation.BIOSDependency, string, error) {
	var resolved corevalidation.BIOSDependency
	var condition, emulatorPath, installationID, blobID, installationStatus sql.NullString
	var installationVersion sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
SELECT q.id,
q.version,
q.catalog_digest,
q.logical_name,
q.requirement_mode,
q.condition_code,
q.delivery_kind,
q.emulator_path,
i.id,
i.version,
i.blob_id,
i.status
FROM bios_requirements q
JOIN bios_installations i ON i.requirement_id=q.id
AND i.is_active=1
AND i.validated_requirement_version=q.version
AND i.status IN ('MATCHED','HASH_WARNING')
WHERE q.core_artifact_id=?
AND q.source_kind='DAT_MACHINE'
AND q.enabled=1
AND q.logical_name=?
`, artifactID, logicalName).Scan(
		&resolved.RequirementID,
		&resolved.RequirementVersion,
		&resolved.CatalogDigest,
		&resolved.LogicalName,
		&resolved.RequirementMode,
		&condition,
		&resolved.DeliveryKind,
		&emulatorPath,
		&installationID,
		&installationVersion,
		&blobID,
		&installationStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("libraryimport/review: resolve arcade BIOS: %w", err)
	}
	resolved.ConditionCode = nullableStringPointer(condition)
	resolved.EmulatorPath = nullableStringPointer(emulatorPath)
	resolved.ActivationOptions = map[string]string{}
	resolved.InstallationID = nullableStringPointer(installationID)
	resolved.InstallationVersion = nullableInt64Pointer(installationVersion)
	resolved.BlobID = nullableStringPointer(blobID)
	resolved.InstallationStatus = nullableStringPointer(installationStatus)
	if installationStatus.String == "HASH_WARNING" {
		return &resolved, "HASH_WARNING", nil
	}
	return &resolved, "SATISFIED_EXTERNAL", nil
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func isStaticBIOSSnapshot(raw string) bool {
	_, err := corevalidation.ParseSnapshot(raw)
	return err == nil
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

func (service *Service) validCandidateAsset(ctx context.Context, transaction *sql.Tx, itemID, assetID string) bool {
	var count int
	err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=?
AND r.import_item_id=?
AND r.state='COMPLETED'
AND a.status='READY'
`, assetID, itemID).
		Scan(&count)
	return err == nil && count == 1
}

func (service *Service) validUploadedAsset(ctx context.Context, transaction *sql.Tx, itemID, assetID string) bool {
	var count int
	err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM review_uploaded_assets
WHERE id=? AND import_item_id=? AND kind='COVER'
`, assetID, itemID).Scan(&count)
	return err == nil && count == 1
}

type DecisionResult struct {
	ItemID      string `json:"itemId"`
	EventID     string `json:"reviewEventId"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Discard(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	reason string,
) (DecisionResult, error) {
	reason = strings.TrimSpace(reason)
	if reason != "" && !validField(reason, 500, true) {
		return DecisionResult{}, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var importID, metadataJSON, sourceManifestJSON, configSnapshotJSON string
	var validationID, datID, dependencySnapshot, candidateID, coverID, uploadedCoverID, backgroundID sql.NullString
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT i.import_job_id,
d.metadata_json,
d.version,
source_snapshot.source_manifest_json,
j.config_snapshot_json,
d.selected_validation_id,
v.dat_version_id,
v.dependency_snapshot_json,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
LEFT JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE i.id=?
AND i.state='REVIEW_PENDING'
`, itemID).Scan(
		&importID,
		&metadataJSON,
		&currentVersion,
		&sourceManifestJSON,
		&configSnapshotJSON,
		&validationID,
		&datID,
		&dependencySnapshot,
		&candidateID,
		&coverID,
		&uploadedCoverID,
		&backgroundID,
	); err != nil ||
		currentVersion != expectedVersion {
		return DecisionResult{}, ErrInvalid
	}
	beforeJSON, _ := json.Marshal(map[string]any{
		"schemaVersion":        1,
		"metadata":             json.RawMessage(metadataJSON),
		"sourceManifest":       json.RawMessage(sourceManifestJSON),
		"selectedValidationId": nullable(validationID),
		"selectedCandidateId":  nullable(candidateID),
		"selectedAssets": map[string]any{
			"coverCandidateAssetId":      nullable(coverID),
			"coverUploadedAssetId":       nullable(uploadedCoverID),
			"backgroundCandidateAssetId": nullable(backgroundID),
		},
	})
	configEvidenceJSON, _ := json.Marshal(map[string]any{
		"schemaVersion":  1,
		"configSnapshot": json.RawMessage(configSnapshotJSON),
		"validationId":   nullable(validationID),
	})
	var dependencyEvidence any
	if dependencySnapshot.Valid {
		dependencyEvidence = json.RawMessage(dependencySnapshot.String)
	}
	datEvidenceJSON, _ := json.Marshal(map[string]any{
		"schemaVersion":      1,
		"datVersionId":       nullable(datID),
		"dependencySnapshot": dependencyEvidence,
	})
	providerEvidenceJSON, _ := json.Marshal(map[string]any{
		"schemaVersion":              1,
		"selectedCandidateId":        nullable(candidateID),
		"coverCandidateAssetId":      nullable(coverID),
		"coverUploadedAssetId":       nullable(uploadedCoverID),
		"backgroundCandidateAssetId": nullable(backgroundID),
	})
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
cancel_requested_at_ms=?,cancel_reason='review discarded',
finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE NULL END,
version=version+1,updated_at_ms=?
WHERE id IN (SELECT job_id FROM review_arcade_parent_attachments
WHERE import_item_id=? AND state IN ('QUEUED','RUNNING'))
AND state IN ('QUEUED','RUNNING')
`, now, now, now, itemID); err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments
SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE import_item_id=? AND state IN ('QUEUED','RUNNING')
`, now, now, itemID); err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='DISCARDED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='REVIEW_PENDING';
 UPDATE import_jobs
SET review_pending_item_count=review_pending_item_count-1,
discarded_item_count=discarded_item_count+1,
state=CASE WHEN review_pending_item_count=1
AND rejected_file_count=resolved_rejected_file_count THEN 'COMPLETED'
WHEN review_pending_item_count=1 THEN 'PARTIAL_FAILURE'
ELSE state END,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN review_pending_item_count=1
AND rejected_file_count=resolved_rejected_file_count THEN ? ELSE NULL END
WHERE id=?
`, now, now, itemID, now, now, importID); err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	eventID, _ := uuid.NewV7()
	actor := reviewActor(ctx)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,
import_item_id,
event_type,
actor_kind,
actor_user_id,
actor_label,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
reason,
created_at_ms) VALUES(?,
?,
'DISCARDED',
?,
?,
?,
?,
'{"schemaVersion":1,"decision":"DISCARDED"}',
'{"schemaVersion":1,"decision":"DISCARDED"}',
?,
?,
?,
?,
?)
`,
		eventID.String(),
		itemID,
		actor.Kind,
		actor.UserID,
		actor.Label,
		string(beforeJSON),
		string(configEvidenceJSON),
		string(datEvidenceJSON),
		string(providerEvidenceJSON),
		nullableText(reason),
		now,
	); err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return DecisionResult{
		ItemID:      itemID,
		EventID:     eventID.String(),
		Status:      "DISCARDED",
		Version:     currentVersion + 1,
		UpdatedAtMS: now,
	}, nil
}

type RetryResult struct {
	ItemID  string `json:"itemId"`
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}

//nolint:funlen // Retry eligibility, execution creation, event emission, and aggregate update share one transaction.
func (service *Service) RetryItem(ctx context.Context, itemID string, expectedVersion int64) (RetryResult, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var importID, stage, manifestDigest string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT import_job_id,
failed_stage,
source_manifest_digest,
version
FROM import_items
WHERE id=?
AND state='FAILED_RETRYABLE'
`, itemID).Scan(&importID, &stage, &manifestDigest, &version); err != nil ||
		version != expectedVersion {
		return RetryResult{}, ErrInvalid
	}
	jobID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte(itemID + ":" + stage + ":" + time.UnixMilli(now).UTC().Format(time.RFC3339Nano)))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'IMPORT_ITEM_PIPELINE',
?,
1,
?,
1,
'QUEUED',
0,
2,
?,
?,
?)
`,
		jobID.String(),
		itemID,
		hex.EncodeToString(dedupe[:]),
		`{"sourceManifestDigest":"`+manifestDigest+`"}`,
		now,
		now,
		now,
	); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='QUEUED',
failed_stage=NULL,
last_error_code=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?;
 UPDATE import_jobs
SET failed_item_count=failed_item_count-1,
queued_item_count=queued_item_count+1,
state='RUNNING',
version=version+1,
updated_at_ms=?
WHERE id=?;
 INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'MANUAL_RETRY',
'{}',
?)
`, now, itemID, now, importID, jobID.String(), itemID, now); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return RetryResult{ItemID: itemID, JobID: jobID.String(), State: "QUEUED", Version: version + 1}, nil
}

type CancelResult struct {
	ImportJobID string `json:"importJobId"`
	State       string `json:"state"`
	Version     int64  `json:"version"`
}

func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	reason string,
) (CancelResult, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || !validField(reason, 500, true) {
		return CancelResult{}, false, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version, running int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version,
running_item_count
FROM import_jobs
WHERE id=?
`, importID).Scan(&state, &version, &running); err != nil ||
		version != expectedVersion ||
		state == "COMPLETED" ||
		state == "CANCELLED" ||
		state == "FAILED" {
		return CancelResult{}, false, ErrInvalid
	}
	now := service.now().UnixMilli()
	pending := running > 0
	newState := "CANCELLED"
	if pending {
		newState = "CANCEL_REQUESTED"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='CANCELLED',
completed_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE import_job_id=?
AND state IN ('QUEUED',
'REVIEW_PENDING',
'FAILED_RETRYABLE');
 UPDATE import_jobs
SET state=?,
cancel_requested_at_ms=?,
cancel_reason=?,
canceled_item_count=canceled_item_count+queued_item_count+review_pending_item_count+failed_item_count,
queued_item_count=0,
review_pending_item_count=0,
failed_item_count=0,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN ?='CANCELLED' THEN ? ELSE NULL END
WHERE id=?
`, now, now, importID, newState, now, reason, now, newState, now, importID); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	return CancelResult{ImportJobID: importID, State: newState, Version: version + 1}, pending, nil
}
