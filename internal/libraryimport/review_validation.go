package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"

	application "retrom/internal/service/libraryimport"

	"retrom/internal/persistence/contentquery"

	validationpersistence "retrom/internal/persistence/corevalidation"
	validationservice "retrom/internal/service/corevalidation"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/contentcapability"

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
	if err := state.resolveDependencies(); err != nil {
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
	providerID              string
	runtimeTargetID         string
	contentPolicy           contentcapability.Policy
	datID                   sql.NullString
	sourceID                string
	sourceManifestDigest    string
	sourceInputDigest       string
	sourceStatus            string
	compatibilityCode       string
	dependencySnapshot      string
	dependencyState         draftDependencyState
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
SELECT binding.provider_id,binding.target_id,
  (SELECT id FROM dat_versions WHERE provider_id=binding.provider_id AND target_id=binding.target_id AND is_active=1),
  `+contentquery.BindingPolicySQL+`
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=?
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE binding.core_id=? AND binding.launch_policy!='DISABLED'
	`, platformID, defaultCoreID).Scan(
		&state.providerID, &state.runtimeTargetID, &state.datID, contentquery.ScanPolicy(&state.contentPolicy),
	)
	if err != nil {
		return ErrInvalid
	}
	return nil
}

func (state *draftValidationRefresh) loadRPGMakerInputs() error {
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT 'rpgmaker',profile.provider_id,profile.target_id,
 (SELECT id FROM dat_versions WHERE provider_id=profile.provider_id AND target_id=profile.target_id AND is_active=1),
 `+contentquery.BindingPolicySQL+`
FROM review_drafts draft
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN runtime_targets target ON target.provider_id=profile.provider_id AND target.target_id=profile.target_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.core_id='rpgmaker' AND binding.launch_policy<>'DISABLED'
WHERE draft.import_item_id=? AND draft.target_platform_instance_id=?
`, state.itemID, state.targetID).Scan(
		&state.coreID,
		&state.providerID,
		&state.runtimeTargetID,
		&state.datID,
		contentquery.ScanPolicy(&state.contentPolicy),
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
  AND core_id=? AND provider_id=? AND target_id=?
  AND dat_version_id IS ? AND default_dos_entry IS ?
ORDER BY created_at_ms DESC,id DESC LIMIT 1
	`, state.itemID, state.effectiveSnapshotID, state.targetID,
		state.coreID, state.providerID, state.runtimeTargetID,
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
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return "", false, err
	}
	state.dependencyState = dependencyState
	if !state.exactValidationCurrent() {
		return "", false, nil
	}
	// Trial-required projects are current even though their status is BLOCKED.
	// Reusing that validation keeps the existing screenshot and approval decision.
	dependenciesUnchanged := !dependencyState.tracked ||
		(dependencyState.snapshotJSON == state.dependencySnapshot &&
			dependencyState.status == state.sourceStatus && dependencyState.code == state.compatibilityCode)
	if !dependenciesUnchanged {
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
		TargetPlatformInstanceID: state.targetID,
		ProviderID:               state.providerID, TargetID: state.runtimeTargetID,
		ContentPolicyDigest: state.contentPolicy.DigestFor(state.contentKind),
		DATVersionID:        nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		DependencySnapshot: json.RawMessage(state.dependencySnapshot), Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode,
	})
}

func (state *draftValidationRefresh) loadFallbackValidation() error {
	if state.sourceID != "" && (state.sourceStatus == "READY" || state.dependencyState.tracked) {
		return nil
	}
	err := state.transaction.QueryRowContext(state.ctx, `
SELECT validation.id,validation.source_manifest_digest,validation.prepublish_input_digest,
  validation.status,validation.compatibility_code,validation.dependency_snapshot_json
FROM import_item_core_validations validation
WHERE validation.import_item_id=? AND validation.source_snapshot_id=? AND validation.core_id=?
  AND validation.provider_id=? AND validation.target_id=? AND validation.dat_version_id IS ?
ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1
`, state.itemID, state.effectiveSnapshotID, state.coreID, state.providerID,
		state.runtimeTargetID, nullable(state.datID)).Scan(
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

func (state *draftValidationRefresh) resolveDependencyState() (draftDependencyState, error) {
	if state.contentKind == "SCUMMVM_PROJECT" {
		return state.resolveScummVMSelection()
	}
	if state.contentKind == "RPG_MAKER_PROJECT" {
		return state.resolveRPGDependencies()
	}
	return resolveDraftBIOSState(state.ctx, state.transaction, state.effectiveSnapshotID, state.providerID,
		state.runtimeTargetID, state.dependencySnapshot, state.sourceStatus, state.compatibilityCode)
}

func (state *draftValidationRefresh) resolveDependencies() error {
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return err
	}
	state.dependencyState = dependencyState
	if dependencyState.tracked {
		state.sourceStatus = dependencyState.status
		state.compatibilityCode = dependencyState.code
		state.dependencySnapshot = dependencyState.snapshotJSON
	}
	return nil
}

func (state *draftValidationRefresh) insertValidation() (string, error) {
	createdID, _ := uuid.NewV7()
	now := state.service.now().UnixMilli()
	digest := prepublishDigest(state.digestInput())
	_, err := recordstore.CreateImportItemCoreValidations(state.ctx, state.transaction, `
INSERT INTO import_item_core_validations(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,
  provider_id,target_id,dat_version_id,
  default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
  status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, createdID.String(), state.itemID, state.targetID, state.platformVersion, state.coreID,
		state.providerID, state.runtimeTargetID, nullable(state.datID),
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
		SchemaVersion: 1, SourceSnapshotID: state.effectiveSnapshotID,
		SourceManifestDigest: state.effectiveManifestDigest, ContentKind: state.contentKind,
		TargetPlatformInstanceID: state.targetID,
		ProviderID:               state.providerID, TargetID: state.runtimeTargetID,
		ContentPolicyDigest: state.contentPolicy.DigestFor(state.contentKind),
		DATVersionID:        nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
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
`, createdID, now, state.sourceID, state.dependencyState.replaceBundle)
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if !state.dependencyState.tracked {
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
	for _, dependency := range state.dependencyState.dependencies {
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

type draftDependencyState struct {
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
	sourceSnapshotID, providerID, targetID, previousSnapshot, previousStatus, previousCode string,
) (draftDependencyState, error) {
	if !isStaticBIOSSnapshot(previousSnapshot) {
		return resolveArcadeDraftBIOSState(
			ctx, transaction, providerID, targetID, previousSnapshot, previousStatus, previousCode,
		)
	}
	logicalName, err := snapshotContentLogicalName(ctx, transaction, sourceSnapshotID)
	if err != nil {
		return draftDependencyState{}, err
	}
	snapshot, status, code, err := validationservice.New(
		validationpersistence.New(
			transaction,
		),
	).ResolveBIOS(
		ctx,
		providerID,
		targetID,
		logicalName,
	)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return draftDependencyState{
		tracked:       true,
		replaceBundle: true,
		snapshotJSON:  string(encoded),
		status:        status,
		code:          code,
		dependencies:  snapshot.BIOS,
	}, nil
}

// snapshotContentLogicalName returns the stable content identity used by
// conditional static dependency rules. DOS snapshots do not have a CONTENT
// row, so their first deterministic DOS_SOURCE is the bundle identity.
func snapshotContentLogicalName(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID string,
) (string, error) {
	var logicalName string
	err := transaction.QueryRowContext(ctx, `
SELECT logical_name
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','DOS_SOURCE')
ORDER BY CASE role WHEN 'CONTENT' THEN 0 WHEN 'DISC' THEN 1 ELSE 2 END,
  sort_order,logical_name
LIMIT 1
`, sourceSnapshotID).Scan(&logicalName)
	if err != nil || logicalName == "" {
		return "", fmt.Errorf("libraryimport/review: %w", ErrInvalid)
	}
	return logicalName, nil
}

type (
	arcadeDraftDependency = application.ArcadeDraftDependency
	arcadeDraftSnapshot   = application.ArcadeDraftSnapshot
)

func resolveArcadeDraftBIOSState(
	ctx context.Context,
	transaction *sql.Tx,
	providerID, targetID, previousSnapshot, previousStatus, previousCode string,
) (draftDependencyState, error) {
	resolved, err := application.ResolveCreationArcade(ctx, repository.BindCreationArcade(transaction),
		providerID, targetID, previousSnapshot, previousStatus, previousCode)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("resolve arcade BIOS: %w", err)
	}
	return draftDependencyState{
		tracked: resolved.Tracked, status: resolved.Status, code: resolved.Code,
		snapshotJSON: resolved.SnapshotJSON, dependencies: resolved.Dependencies,
	}, nil
}

func parseArcadeDraftSnapshot(raw string) (arcadeDraftSnapshot, bool) {
	return application.ParseArcadeDraftSnapshot(raw)
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
