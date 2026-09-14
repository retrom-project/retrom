package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	corevalidationmodel "retrom/internal/model/corevalidation"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/contentquery"
	corevalidationrepo "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/recordstore"
)

// Inputs resolves the source snapshot and the selected runtime target in the
// caller's transaction. Keeping this projection in persistence means the
// refresh rules can work with a stable application value object.
func (records *ReviewValidation) Inputs(
	ctx context.Context, itemID, targetID string,
) (application.ReviewValidationRefreshInputs, error) {
	var result application.ReviewValidationRefreshInputs
	var platformID, defaultCoreID string
	var datVersionID sql.NullString
	if err := records.executor.QueryRowContext(ctx, `
SELECT draft.id,snapshot.id,snapshot.source_manifest_digest,snapshot.content_kind
FROM review_drafts draft
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE draft.import_item_id=?
`, itemID).Scan(
		&result.DraftID, &result.EffectiveSnapshotID, &result.EffectiveManifestDigest, &result.ContentKind,
	); err != nil {
		return application.ReviewValidationRefreshInputs{}, application.ErrInvalid
	}
	if err := records.executor.QueryRowContext(ctx, `
SELECT version,platform_id,default_core_id
FROM platform_instances
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL
	`, targetID).Scan(&result.PlatformVersion, &platformID, &defaultCoreID); err != nil {
		return application.ReviewValidationRefreshInputs{}, application.ErrInvalid
	}
	result.PlatformID = platformID
	if platformID == "rpgmaker" {
		return records.rpgInputs(ctx, itemID, targetID, result)
	}
	result.CoreID = defaultCoreID
	if err := records.executor.QueryRowContext(ctx, `
SELECT binding.provider_id,binding.target_id,
  (SELECT id FROM dat_versions WHERE provider_id=binding.provider_id AND target_id=binding.target_id AND is_active=1),
  `+contentquery.BindingPolicySQL+`
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=?
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE binding.core_id=? AND binding.launch_policy!='DISABLED'
	`, platformID, defaultCoreID).Scan(
		&result.ProviderID, &result.RuntimeTargetID, &datVersionID,
		contentquery.ScanPolicy(&result.ContentPolicy),
	); err != nil {
		return application.ReviewValidationRefreshInputs{}, application.ErrInvalid
	}
	result.DATVersionID = nullableReviewValidationString(datVersionID)
	dependencyDigest, err := records.dependencyFactsDigest(
		ctx, itemID, result.ProviderID, result.RuntimeTargetID, result.ContentKind,
	)
	if err != nil {
		return application.ReviewValidationRefreshInputs{}, application.ErrInvalid
	}
	result.DependencyFactsDigest = dependencyDigest
	return result, nil
}

func (records *ReviewValidation) rpgInputs(
	ctx context.Context,
	itemID, targetID string,
	result application.ReviewValidationRefreshInputs,
) (application.ReviewValidationRefreshInputs, error) {
	var datVersionID sql.NullString
	if err := records.executor.QueryRowContext(ctx, `
SELECT 'rpgmaker',profile.provider_id,profile.target_id,
 (SELECT id FROM dat_versions WHERE provider_id=profile.provider_id AND target_id=profile.target_id AND is_active=1),
 `+contentquery.BindingPolicySQL+`
FROM review_drafts draft
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN runtime_targets target ON target.provider_id=profile.provider_id AND target.target_id=profile.target_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.core_id='rpgmaker' AND binding.launch_policy<>'DISABLED'
WHERE draft.import_item_id=? AND draft.target_platform_instance_id=?
	`, itemID, targetID).Scan(
		&result.CoreID, &result.ProviderID, &result.RuntimeTargetID,
		&datVersionID, contentquery.ScanPolicy(&result.ContentPolicy),
	); err != nil {
		return application.ReviewValidationRefreshInputs{}, application.ErrInvalid
	}
	result.DATVersionID = nullableReviewValidationString(datVersionID)
	dependencyDigest, err := records.dependencyFactsDigest(
		ctx, itemID, result.ProviderID, result.RuntimeTargetID, result.ContentKind,
	)
	if err != nil {
		return application.ReviewValidationRefreshInputs{}, application.ErrInvalid
	}
	result.DependencyFactsDigest = dependencyDigest
	return result, nil
}

func nullableReviewValidationString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func (records *ReviewValidation) Exact(
	ctx context.Context, lookup application.ReviewValidationRefreshLookup,
) (application.ReviewValidationRefreshRecord, bool, error) {
	var result application.ReviewValidationRefreshRecord
	err := records.executor.QueryRowContext(ctx, `
SELECT id,source_manifest_digest,prepublish_input_digest,status,
  compatibility_code,dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=? AND source_snapshot_id=? AND target_platform_instance_id=?
  AND core_id=? AND provider_id=? AND target_id=?
  AND dat_version_id IS ? AND default_dos_entry IS ?
ORDER BY created_at_ms DESC,id DESC LIMIT 1
	`, lookup.ItemID, lookup.SourceSnapshotID, lookup.TargetPlatformInstanceID,
		lookup.CoreID, lookup.ProviderID, lookup.TargetID,
		reviewValidationArgument(lookup.DATVersionID), reviewValidationArgument(lookup.DefaultDOSEntry)).Scan(
		&result.ID, &result.SourceManifestDigest, &result.PrepublishInputDigest,
		&result.Status, &result.CompatibilityCode, &result.DependencySnapshot,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewValidationRefreshRecord{}, false, nil
	}
	if err != nil {
		return application.ReviewValidationRefreshRecord{}, false, fmt.Errorf("query exact review validation: %w", err)
	}
	return result, true, nil
}

func (records *ReviewValidation) Fallback(
	ctx context.Context, lookup application.ReviewValidationRefreshLookup,
) (application.ReviewValidationRefreshRecord, bool, error) {
	var result application.ReviewValidationRefreshRecord
	err := records.executor.QueryRowContext(ctx, `
SELECT validation.id,validation.source_manifest_digest,validation.prepublish_input_digest,
  validation.status,validation.compatibility_code,validation.dependency_snapshot_json
FROM import_item_core_validations validation
WHERE validation.import_item_id=? AND validation.source_snapshot_id=? AND validation.core_id=?
  AND validation.provider_id=? AND validation.target_id=? AND validation.dat_version_id IS ?
ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1
`, lookup.ItemID, lookup.SourceSnapshotID, lookup.CoreID, lookup.ProviderID,
		lookup.TargetID, reviewValidationArgument(lookup.DATVersionID)).Scan(
		&result.ID, &result.SourceManifestDigest, &result.PrepublishInputDigest,
		&result.Status, &result.CompatibilityCode, &result.DependencySnapshot,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewValidationRefreshRecord{}, false, nil
	}
	if err != nil {
		return application.ReviewValidationRefreshRecord{}, false, fmt.Errorf("query fallback review validation: %w", err)
	}
	return result, true, nil
}

func (records *ReviewValidation) Create(
	ctx context.Context, value application.ReviewValidationRefreshCreate,
) error {
	_, err := recordstore.CreateImportItemCoreValidations(ctx, records.executor, `
INSERT INTO import_item_core_validations(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,
  provider_id,target_id,dat_version_id,
  default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
  status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, value.ID, value.ItemID, value.TargetPlatformInstanceID, value.PlatformInstanceVersion, value.CoreID,
		value.ProviderID, value.TargetID, reviewValidationArgument(value.DATVersionID),
		reviewValidationArgument(value.DefaultDOSEntry), value.SourceManifestDigest, value.SourceSnapshotID,
		value.PrepublishInputDigest, value.Status, value.CompatibilityCode,
		value.DependencySnapshotJSON, value.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("create review validation: %w", err)
	}
	return nil
}

func (records *ReviewValidation) CopyFiles(
	ctx context.Context, value application.ReviewValidationRefreshFileCopy,
) error {
	_, err := records.executor.ExecContext(ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
)
SELECT ?,role,logical_name,blob_id,sort_order,?
FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND (?=0 OR role<>'BIOS_BUNDLE')
`, value.ValidationID, value.CreatedAtMS, value.SourceValidationID, boolNumber(value.ReplaceBIOSBundle))
	if err != nil {
		return fmt.Errorf("copy review validation files: %w", err)
	}
	if !value.ReplaceBIOSBundle && len(value.Dependencies) == 0 {
		return nil
	}
	var sortOrder int
	if err := records.executor.QueryRowContext(ctx, `
SELECT COALESCE(MAX(sort_order),-1)+1
FROM import_item_validation_files
WHERE import_item_core_validation_id=?
`, value.ValidationID).Scan(&sortOrder); err != nil {
		return fmt.Errorf("query review validation file order: %w", err)
	}
	for _, dependency := range value.Dependencies {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		if _, err := records.executor.ExecContext(ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
) VALUES(?,'BIOS_BUNDLE',?,?,?,?)
`, value.ValidationID, dependency.LogicalName, *dependency.BlobID, sortOrder, value.CreatedAtMS); err != nil {
			return fmt.Errorf("insert review validation BIOS file: %w", err)
		}
		sortOrder++
	}
	return nil
}

func boolNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}

func reviewValidationArgument(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func (records *ReviewValidation) ContentLogicalName(ctx context.Context, snapshotID string) (string, error) {
	var logicalName string
	err := records.executor.QueryRowContext(ctx, `
SELECT logical_name
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','DOS_SOURCE')
ORDER BY CASE role WHEN 'CONTENT' THEN 0 WHEN 'DISC' THEN 1 ELSE 2 END,
  sort_order,logical_name
LIMIT 1
`, snapshotID).Scan(&logicalName)
	if err != nil || logicalName == "" {
		return "", fmt.Errorf("read review validation content identity: %w", application.ErrInvalid)
	}
	return logicalName, nil
}

func (records *ReviewValidation) RPGProfile(
	ctx context.Context, draftID string,
) (application.RPGReviewProfile, error) {
	var result application.RPGReviewProfile
	err := records.executor.QueryRowContext(ctx, `
SELECT generation,self_contained_override,dependency_snapshot_sha256,analysis_json
FROM rpgmaker_review_profiles WHERE review_draft_id=?
`, draftID).Scan(
		&result.Generation, &result.SelfContainedOverride,
		&result.DependencySHA256, &result.AnalysisJSON,
	)
	if err != nil {
		return application.RPGReviewProfile{}, application.ErrInvalid
	}
	var analysis application.RPGReviewAnalysis
	if err := json.Unmarshal([]byte(result.AnalysisJSON), &analysis); err != nil {
		return application.RPGReviewProfile{}, fmt.Errorf("decode RPG review profile: %w", application.ErrInvalid)
	}
	return result, nil
}

func (records *ReviewValidation) UpdateRPGDependencyDigest(
	ctx context.Context, draftID, digest string, nowMS int64,
) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE rpgmaker_review_profiles SET dependency_snapshot_sha256=?,updated_at_ms=? WHERE review_draft_id=?
`, digest, nowMS, draftID); err != nil {
		return fmt.Errorf("update RPG dependency digest: %w", err)
	}
	return nil
}

// BIOS exposes only the facts needed by the application validation planner.
// The planner receives values and never receives this executor.
func (records *ReviewValidation) BIOS(
	ctx context.Context, providerID, targetID string,
) ([]corevalidationmodel.BIOSRecord, error) {
	result, err := corevalidationrepo.New(records.executor).BIOS(ctx, providerID, targetID)
	if err != nil {
		return nil, fmt.Errorf("read review validation BIOS: %w", err)
	}
	return result, nil
}

func (records *ReviewValidation) ArcadeBIOS(
	ctx context.Context, providerID, targetID, logicalName string,
) (corevalidation.BIOSDependency, bool, error) {
	dependency, found, err := BindCreationArcade(records.executor).BIOS(ctx, providerID, targetID, logicalName)
	if err != nil {
		return corevalidation.BIOSDependency{}, false, fmt.Errorf("read review validation arcade BIOS: %w", err)
	}
	return dependency, found, nil
}

var (
	_ application.ReviewValidationRefreshRepository = (*ReviewValidation)(nil)
	_ application.ReviewValidationRefreshReader     = (*ReviewValidation)(nil)
)
