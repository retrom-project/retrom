package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
	libraryservice "retrom/internal/service/libraryimport"
)

// Inputs resolves the source snapshot and the selected runtime target in the
// caller's transaction. Keeping this projection in persistence means the
// current checks can work with a stable application value object.
func (records *ReviewInputs) Inputs(
	ctx context.Context, itemID, targetID string,
) (libraryservice.ReviewRuntimeInputs, error) {
	var result libraryservice.ReviewRuntimeInputs
	var platformID, defaultCoreID string
	var datVersionID sql.NullString
	if err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT draft.id,snapshot.id,snapshot.source_manifest_digest,snapshot.content_kind
FROM import_items draft
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE draft.id=?
`, itemID).Scan(
		&result.DraftID, &result.EffectiveSnapshotID, &result.EffectiveManifestDigest, &result.ContentKind,
	); err != nil {
		return libraryservice.ReviewRuntimeInputs{}, libraryservice.ErrInvalid
	}
	if err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT version,platform_id,default_core_id
FROM platform_instances
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL
`, targetID).Scan(&result.PlatformVersion, &platformID, &defaultCoreID); err != nil {
		return libraryservice.ReviewRuntimeInputs{}, libraryservice.ErrInvalid
	}
	if platformID == "rpgmaker" {
		return records.rpgInputs(ctx, itemID, targetID, result)
	}
	result.CoreID = defaultCoreID
	if err := dbapi.QueryRowContext(
		ctx,
		records.executor,
		`
SELECT binding.provider_id,binding.target_id,
  (SELECT id FROM dat_versions WHERE provider_id=binding.provider_id AND target_id=binding.target_id AND
is_active=1),
  `+contentquery.BindingPolicySQL+`
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=?
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE binding.core_id=? AND binding.launch_policy!='DISABLED'
	`,
		platformID,
		defaultCoreID,
	).Scan(

		&result.ProviderID,
		&result.RuntimeTargetID,
		&datVersionID,

		contentquery.ScanPolicy(&result.ContentPolicy),
	); err != nil {
		return libraryservice.ReviewRuntimeInputs{}, libraryservice.ErrInvalid
	}
	result.DATVersionID = nullableReviewInputString(datVersionID)
	return result, nil
}

func (records *ReviewInputs) rpgInputs(
	ctx context.Context,
	itemID, targetID string,
	result libraryservice.ReviewRuntimeInputs,
) (libraryservice.ReviewRuntimeInputs, error) {
	profile, err := readRPGReviewProfile(ctx, records.executor, itemID)
	if err != nil {
		return libraryservice.ReviewRuntimeInputs{}, libraryservice.ErrInvalid
	}
	var datVersionID sql.NullString
	if err := dbapi.QueryRowContext(
		ctx,
		records.executor,
		`
SELECT 'rpgmaker',target.provider_id,target.target_id,
 (SELECT id FROM dat_versions WHERE provider_id=target.provider_id
 AND target_id=target.target_id AND is_active=1),
 `+contentquery.BindingPolicySQL+`
FROM import_items draft
JOIN runtime_targets target ON target.provider_id=? AND target.target_id=?
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.core_id='rpgmaker' AND binding.launch_policy<>'DISABLED'
WHERE draft.id=? AND draft.target_platform_instance_id=?
	`,
		profile.ProviderID,
		profile.TargetID,
		itemID,
		targetID,
	).Scan(

		&result.CoreID,
		&result.ProviderID,
		&result.RuntimeTargetID,

		&datVersionID,
		contentquery.ScanPolicy(&result.ContentPolicy),
	); err != nil {
		return libraryservice.ReviewRuntimeInputs{}, libraryservice.ErrInvalid
	}
	result.DATVersionID = nullableReviewInputString(datVersionID)
	return result, nil
}

func nullableReviewInputString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func (records *ReviewInputs) ContentLogicalName(ctx context.Context, snapshotID string) (string, error) {
	var logicalName string
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT logical_name
FROM (
 SELECT source_reference AS logical_name,0 AS priority,ordinal AS sort_order
 FROM import_item_multidisc_entries WHERE source_snapshot_id=?
 UNION ALL
 SELECT logical_name,CASE role WHEN 'CONTENT' THEN 1 WHEN 'DISC' THEN 2 ELSE 3 END,sort_order
 FROM import_item_source_snapshot_files
 WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','DOS_SOURCE','PROJECT_FILE')
)
ORDER BY priority,sort_order,logical_name
LIMIT 1
`, snapshotID, snapshotID).Scan(&logicalName)
	if err != nil || logicalName == "" {
		return "", fmt.Errorf("read review content identity: %w", libraryservice.ErrInvalid)
	}
	return logicalName, nil
}

func (records *ReviewInputs) RPGProfile(
	ctx context.Context, draftID string,
) (libraryservice.RPGReviewProfile, error) {
	profile, err := readRPGReviewProfile(ctx, records.executor, draftID)
	if err != nil {
		return libraryservice.RPGReviewProfile{}, libraryservice.ErrInvalid
	}
	result := libraryservice.RPGReviewProfile{
		Generation: profile.Generation, SelfContainedOverride: profile.SelfContainedOverride != 0,
		DependencySHA256: profile.DependencySnapshotSHA256, AnalysisJSON: string(profile.Analysis),
	}
	var analysis libraryservice.RPGReviewAnalysis
	if err := json.Unmarshal([]byte(result.AnalysisJSON), &analysis); err != nil {
		return libraryservice.RPGReviewProfile{}, fmt.Errorf("decode RPG review profile: %w", libraryservice.ErrInvalid)
	}
	return result, nil
}
