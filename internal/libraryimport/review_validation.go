package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"retrom/internal/corevalidation"

	"github.com/google/uuid"
)

// Keep immutable validation refresh branches together for auditability.
func (service *Service) ensureCompatibleDraftValidation(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, targetID string,
	dosEntry sql.NullString,
) (string, error) {
	state := draftValidationRefresh{
		service: service, ctx: ctx, transaction: transaction,
		itemID: itemID, targetID: targetID, dosEntry: dosEntry,
	}
	if err := state.loadInputs(); err != nil {
		return "", err
	}
	selected, current, err := state.loadExactValidation()
	if err != nil || current {
		return selected, err
	}
	if err := state.loadFallbackValidation(); err != nil {
		return "", err
	}
	if state.sourceID == "" {
		return "", nil
	}
	if err := state.resolveBIOSState(); err != nil {
		return "", err
	}
	return state.insertValidation()
}

type draftValidationRefresh struct {
	service                 *Service
	ctx                     context.Context
	transaction             *sql.Tx
	itemID                  string
	targetID                string
	dosEntry                sql.NullString
	effectiveSnapshotID     string
	effectiveManifestDigest string
	contentKind             string
	platformVersion         int64
	coreID                  string
	artifactID              string
	artifactVersion         int64
	compatibilityConfig     string
	datID                   sql.NullString
	sourceID                string
	sourceManifestDigest    string
	sourceInputDigest       string
	sourceStatus            string
	compatibilityCode       string
	dependencySnapshot      string
	biosState               draftBIOSState
}

func (state *draftValidationRefresh) loadInputs() error {
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT snapshot.id,snapshot.source_manifest_digest,snapshot.content_kind
FROM review_drafts draft
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE draft.import_item_id=?
`, state.itemID).Scan(
		&state.effectiveSnapshotID, &state.effectiveManifestDigest, &state.contentKind,
	)
	if err != nil {
		return ErrInvalid
	}
	var platformID, defaultCoreID string
	err = state.transaction.QueryRowContext(state.ctx, `
SELECT version,platform_id,default_core_id
FROM platform_instances
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL
`, state.targetID).Scan(&state.platformVersion, &platformID, &defaultCoreID)
	if err != nil {
		return ErrInvalid
	}
	if platformID == "rpgmaker" {
		return state.loadRPGMakerInputs()
	}
	state.coreID = defaultCoreID
	err = state.transaction.QueryRowContext(state.ctx, `
SELECT a.id,a.version,a.compatibility_json,
  (SELECT id FROM dat_versions WHERE core_artifact_id=a.id AND is_active=1)
FROM core_artifacts a
WHERE a.core_id=? AND a.selected_for_new_bindings=1
`, defaultCoreID).Scan(
		&state.artifactID, &state.artifactVersion, &state.compatibilityConfig, &state.datID,
	)
	if err != nil {
		return ErrInvalid
	}
	return nil
}

func (state *draftValidationRefresh) loadRPGMakerInputs() error {
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT profile.selected_core_id,artifact.id,artifact.version,artifact.compatibility_json,
 (SELECT id FROM dat_versions WHERE core_artifact_id=artifact.id AND is_active=1)
FROM review_drafts draft
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN core_artifacts artifact ON artifact.id=profile.artifact_id
 AND artifact.core_id=profile.selected_core_id
 AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
WHERE draft.import_item_id=? AND draft.target_platform_instance_id=?
`, state.itemID, state.targetID).Scan(
		&state.coreID, &state.artifactID, &state.artifactVersion,
		&state.compatibilityConfig, &state.datID,
	)
	if err != nil {
		return ErrInvalid
	}
	return nil
}

func (state *draftValidationRefresh) loadExactValidation() (string, bool, error) {
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT id,source_manifest_digest,prepublish_input_digest,status,
  compatibility_code,dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=? AND source_snapshot_id=? AND target_platform_instance_id=?
  AND platform_instance_version=? AND core_id=? AND core_artifact_id=?
  AND core_artifact_version=? AND prepublish_generation=4
  AND dat_version_id IS ? AND default_dos_entry IS ?
ORDER BY created_at_ms DESC,id DESC LIMIT 1
`, state.itemID, state.effectiveSnapshotID, state.targetID, state.platformVersion,
		state.coreID, state.artifactID, state.artifactVersion,
		nullable(state.datID), nullable(state.dosEntry)).Scan(
		&state.sourceID, &state.sourceManifestDigest, &state.sourceInputDigest,
		&state.sourceStatus, &state.compatibilityCode, &state.dependencySnapshot,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("libraryimport/review: %w", err)
	}
	biosState, err := resolveDraftBIOSState(
		state.ctx, state.transaction, state.effectiveSnapshotID, state.artifactID,
		state.dependencySnapshot, state.sourceStatus, state.compatibilityCode,
	)
	if err != nil {
		return "", false, err
	}
	state.biosState = biosState
	if !state.exactValidationCurrent() {
		return "", false, nil
	}
	if !biosState.tracked && state.sourceStatus == "READY" {
		return state.sourceID, true, nil
	}
	biosUnchanged := biosState.tracked && biosState.snapshotJSON == state.dependencySnapshot &&
		biosState.status == state.sourceStatus && biosState.code == state.compatibilityCode
	if !biosUnchanged {
		return "", false, nil
	}
	if state.sourceStatus == "READY" {
		return state.sourceID, true, nil
	}
	return "", true, nil
}

func (state *draftValidationRefresh) exactValidationCurrent() bool {
	return prepublishDigestMatches(state.sourceInputDigest, prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: state.effectiveSnapshotID,
		SourceManifestDigest: state.sourceManifestDigest, ContentKind: state.contentKind,
		TargetPlatformInstanceID: state.targetID, PlatformInstanceVersion: state.platformVersion,
		CoreArtifactID: state.artifactID, CoreArtifactVersion: state.artifactVersion,
		CompatibilityConfigDigest: compatibilityConfigDigest(state.compatibilityConfig),
		DATVersionID:              nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		DependencySnapshot: json.RawMessage(state.dependencySnapshot), Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode,
	})
}

func (state *draftValidationRefresh) loadFallbackValidation() error {
	if state.sourceID != "" && (state.sourceStatus == "READY" || state.biosState.tracked) {
		return nil
	}
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT id,source_manifest_digest,prepublish_input_digest,status,
  compatibility_code,dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=? AND source_snapshot_id=? AND core_id=? AND core_artifact_id=?
  AND dat_version_id IS ? AND status='READY'
ORDER BY created_at_ms DESC,id DESC LIMIT 1
`, state.itemID, state.effectiveSnapshotID, state.coreID, state.artifactID, nullable(state.datID)).Scan(
		&state.sourceID, &state.sourceManifestDigest, &state.sourceInputDigest,
		&state.sourceStatus, &state.compatibilityCode, &state.dependencySnapshot,
	)
	if errors.Is(err, sql.ErrNoRows) {
		state.sourceID = ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	return nil
}

func (state *draftValidationRefresh) resolveBIOSState() error {
	biosState, err := resolveDraftBIOSState(
		state.ctx, state.transaction, state.effectiveSnapshotID, state.artifactID,
		state.dependencySnapshot, state.sourceStatus, state.compatibilityCode,
	)
	if err != nil {
		return err
	}
	state.biosState = biosState
	if biosState.tracked {
		state.sourceStatus = biosState.status
		state.compatibilityCode = biosState.code
		state.dependencySnapshot = biosState.snapshotJSON
	}
	return nil
}

func (state *draftValidationRefresh) insertValidation() (string, error) {
	createdID, _ := uuid.NewV7()
	now := state.service.now().UnixMilli()
	digest := prepublishDigest(state.digestInput())
	_, err := state.transaction.ExecContext(state.ctx, `
INSERT INTO import_item_core_validations(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,
  core_artifact_id,core_artifact_version,prepublish_generation,dat_version_id,
  default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
  status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, createdID.String(), state.itemID, state.targetID, state.platformVersion, state.coreID,
		state.artifactID, state.artifactVersion, prepublishGeneration, nullable(state.datID),
		nullable(state.dosEntry), state.effectiveManifestDigest, state.effectiveSnapshotID,
		digest, state.sourceStatus, state.compatibilityCode, state.dependencySnapshot, now)
	if err != nil {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	if err := state.copyValidationFiles(createdID.String(), now); err != nil {
		return "", err
	}
	if state.sourceStatus != "READY" {
		return "", nil
	}
	return createdID.String(), nil
}

func (state *draftValidationRefresh) digestInput() prepublishDigestInput {
	return prepublishDigestInput{
		SchemaVersion: 1, ValidatorVersion: validatorReviewV4,
		SourceSnapshotID:     state.effectiveSnapshotID,
		SourceManifestDigest: state.effectiveManifestDigest, ContentKind: state.contentKind,
		TargetPlatformInstanceID: state.targetID, PlatformInstanceVersion: state.platformVersion,
		CoreArtifactID: state.artifactID, CoreArtifactVersion: state.artifactVersion,
		CompatibilityConfigDigest: compatibilityConfigDigest(state.compatibilityConfig),
		DATVersionID:              nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		DependencySnapshot: json.RawMessage(state.dependencySnapshot), Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode,
	}
}

func (state *draftValidationRefresh) copyValidationFiles(createdID string, now int64) error {
	_, err := state.transaction.ExecContext(state.ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
)
SELECT ?,role,logical_name,blob_id,sort_order,?
FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND (?=0 OR role<>'BIOS_BUNDLE')
`, createdID, now, state.sourceID, state.biosState.replaceBundle)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if !state.biosState.tracked {
		return nil
	}
	return state.insertBIOSValidationFiles(createdID, now)
}

func (state *draftValidationRefresh) insertBIOSValidationFiles(createdID string, now int64) error {
	var sortOrder int
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT COALESCE(MAX(sort_order),-1)+1
FROM import_item_validation_files
WHERE import_item_core_validation_id=?
`, createdID).Scan(&sortOrder)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	for _, dependency := range state.biosState.dependencies {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		_, err := state.transaction.ExecContext(state.ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
) VALUES(?,'BIOS_BUNDLE',?,?,?,?)
`, createdID, dependency.LogicalName, *dependency.BlobID, sortOrder, now)
		if err != nil {
			return fmt.Errorf("libraryimport/review: %w", err)
		}
		sortOrder++
	}
	return nil
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
		snapshot.SchemaVersion != 2 || snapshot.Machine == "" ||
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
