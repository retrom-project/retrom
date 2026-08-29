package dependencies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

func bootstrapRPGMaker(
	ctx context.Context,
	transaction *sql.Tx,
	version *RPGMakerVersion,
	now time.Time,
) error {
	if version == nil {
		return fmt.Errorf("%w: RPG Maker runtime unavailable", ErrInvalid)
	}
	declared := make(map[rpgMakerArtifactIdentity]struct{}, len(version.Manifest.Artifacts))
	for index, artifact := range version.Manifest.Artifacts {
		declared[rpgMakerArtifactKey(artifact)] = struct{}{}
		if err := validateRPGMakerGameCompatibility(ctx, transaction, artifact); err != nil {
			return err
		}
		if err := retireConflictingRPGMakerArtifacts(ctx, transaction, artifact, now); err != nil {
			return err
		}
		if _, err := persistRPGMakerArtifact(ctx, transaction, version, index, artifact, now); err != nil {
			return err
		}
	}
	return retireUndeclaredRPGMakerArtifacts(ctx, transaction, declared, now)
}

type rpgMakerArtifactIdentity struct {
	coreID            string
	routeKey          string
	artifactSetSHA256 string
}

func rpgMakerArtifactKey(artifact RPGMakerArtifact) rpgMakerArtifactIdentity {
	return rpgMakerArtifactIdentity{
		coreID:            artifact.CoreID,
		routeKey:          artifact.RouteKey,
		artifactSetSHA256: artifact.ArtifactSetSHA256,
	}
}

func retireUndeclaredRPGMakerArtifacts(
	ctx context.Context,
	transaction *sql.Tx,
	declared map[rpgMakerArtifactIdentity]struct{},
	now time.Time,
) error {
	rows, err := transaction.QueryContext(ctx, `
SELECT id,core_id,route_key,artifact_set_sha256
FROM core_artifacts
WHERE runtime_family IN ('RPGMAKER','ONS') AND (selected_for_new_bindings=1 OR available_for_launch=1)
`)
	if err != nil {
		return fmt.Errorf("list RPG Maker artifacts for retirement: %w", err)
	}
	defer func() { cleanup.Error("close RPG Maker artifact retirement rows", rows.Close()) }()
	var retiredIDs []string
	for rows.Next() {
		var id string
		var identity rpgMakerArtifactIdentity
		if err := rows.Scan(&id, &identity.coreID, &identity.routeKey, &identity.artifactSetSHA256); err != nil {
			return fmt.Errorf("scan RPG Maker artifact for retirement: %w", err)
		}
		if _, exists := declared[identity]; !exists {
			retiredIDs = append(retiredIDs, id)
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close RPG Maker artifact retirement rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate RPG Maker artifacts for retirement: %w", err)
	}
	for _, id := range retiredIDs {
		if err := validateRPGMakerArtifactRetirement(ctx, transaction, id); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts
SET selected_for_new_bindings=0,available_for_launch=0,version=version+1,updated_at_ms=?
WHERE id=? AND (selected_for_new_bindings=1 OR available_for_launch=1)
`, now.UnixMilli(), id); err != nil {
			return fmt.Errorf("retire undeclared RPG Maker artifact: %w", err)
		}
	}
	return nil
}

func validateRPGMakerGameCompatibility(
	ctx context.Context,
	transaction *sql.Tx,
	artifact RPGMakerArtifact,
) error {
	var compatibility struct {
		GameCompatibilityLine string `json:"gameCompatibilityLine"`
	}
	if err := json.Unmarshal(artifact.Compatibility, &compatibility); err != nil ||
		compatibility.GameCompatibilityLine == "" {
		return fmt.Errorf("%w: RPG Maker game compatibility line", ErrInvalid)
	}
	var conflicts int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM game_variant_revisions revision
JOIN core_artifacts bound ON bound.id=revision.core_artifact_id
WHERE revision.status='READY' AND bound.runtime_family=? AND bound.core_id=? AND bound.route_key=?
AND json_extract(bound.compatibility_json,'$.gameCompatibilityLine') IS NOT ?
`, artifact.RuntimeFamily, artifact.CoreID, artifact.RouteKey, compatibility.GameCompatibilityLine).
		Scan(&conflicts); err != nil {
		return fmt.Errorf("check RPG Maker game compatibility: %w", err)
	}
	if conflicts != 0 {
		return fmt.Errorf("%w: RPG Maker game compatibility line changed", ErrInvalid)
	}
	return nil
}

func validateRPGMakerArtifactRetirement(ctx context.Context, transaction *sql.Tx, artifactID string) error {
	var incompatible int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM game_variant_revisions revision
JOIN core_artifacts retired ON retired.id=revision.core_artifact_id
WHERE retired.id=? AND revision.status='READY' AND NOT EXISTS(
  SELECT 1 FROM core_artifacts current
  WHERE current.core_id=retired.core_id AND current.route_key=retired.route_key
    AND current.runtime_family=retired.runtime_family
    AND current.selected_for_new_bindings=1 AND current.available_for_launch=1
    AND json_extract(current.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(retired.compatibility_json,'$.gameCompatibilityLine')
)
`, artifactID).Scan(&incompatible); err != nil {
		return fmt.Errorf("check RPG Maker artifact retirement: %w", err)
	}
	if incompatible != 0 {
		return fmt.Errorf("%w: RPG Maker runtime removal would strand games", ErrInvalid)
	}
	return nil
}

func retireConflictingRPGMakerArtifacts(
	ctx context.Context,
	transaction *sql.Tx,
	artifact RPGMakerArtifact,
	now time.Time,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts
SET selected_for_new_bindings=0,available_for_launch=0,version=version+1,updated_at_ms=?
WHERE runtime_family=? AND core_id=? AND route_key=? AND artifact_set_sha256<>?
AND (selected_for_new_bindings=1 OR available_for_launch=1)
`, now.UnixMilli(), artifact.RuntimeFamily, artifact.CoreID, artifact.RouteKey,
		artifact.ArtifactSetSHA256); err != nil {
		return fmt.Errorf("retire conflicting RPG Maker artifact: %w", err)
	}
	return nil
}

func persistRPGMakerArtifact(
	ctx context.Context,
	transaction *sql.Tx,
	version *RPGMakerVersion,
	index int,
	artifact RPGMakerArtifact,
	now time.Time,
) (string, error) {
	compatibility, err := canonicalJSONObject(artifact.Compatibility)
	if err != nil {
		return "", err
	}
	provenance, _ := json.Marshal(map[string]any{
		"schemaVersion":            2,
		"dependencyManifestSha256": version.ManifestSHA256,
		"manifestEntryPointer":     fmt.Sprintf("/artifacts/%d", index),
		"runtimeRelease":           releaseProvenance(version.Manifest.Release),
	})
	var id string
	err = transaction.QueryRowContext(ctx, `
SELECT id FROM core_artifacts
WHERE core_id=? AND route_key=? AND artifact_set_sha256=?
`, artifact.CoreID, artifact.RouteKey, artifact.ArtifactSetSHA256).Scan(&id)
	existing := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		generated, generateErr := uuid.NewV7()
		if generateErr != nil {
			return "", fmt.Errorf("generate RPG Maker artifact id: %w", generateErr)
		}
		id = generated.String()
	} else if err != nil {
		return "", fmt.Errorf("find RPG Maker artifact: %w", err)
	}
	selected := boolToInteger(artifact.SelectedForNewBindings)
	available := boolToInteger(artifact.AvailableForLaunch)
	if selected == 1 {
		if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts
SET selected_for_new_bindings=0,version=version+1,updated_at_ms=?
WHERE core_id=? AND selected_for_new_bindings=1 AND id<>?
`, now.UnixMilli(), artifact.CoreID, id); err != nil {
			return "", fmt.Errorf("deselect prior RPG Maker artifact: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO core_artifacts(
 id,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,
 entry_path,size_bytes,sha256,manifest_sha256,artifact_set_sha256,requires_threads,
 save_payload_kind,save_max_bytes,provenance_json,compatibility_json,
 selected_for_new_bindings,available_for_launch,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)
ON CONFLICT(core_id,route_key,artifact_set_sha256) DO NOTHING
`, id, artifact.CoreID, artifact.RouteKey, artifact.RuntimeFamily, artifact.RuntimeAdapterKind, artifact.RuntimeVersion,
		artifact.AdapterID, artifact.EntryPath, artifact.EntrySizeBytes, artifact.EntrySHA256,
		version.ManifestSHA256, artifact.ArtifactSetSHA256, boolToInteger(artifact.RequiresThreads),
		artifact.SavePayloadKind, artifact.SaveMaxBytes, string(provenance), string(compatibility),
		selected, available, now.UnixMilli(), now.UnixMilli()); err != nil {
		return "", fmt.Errorf("insert RPG Maker artifact: %w", err)
	}
	var storedManifest, storedProvenance string
	if err := transaction.QueryRowContext(ctx, `
SELECT id,manifest_sha256,provenance_json FROM core_artifacts
WHERE core_id=? AND route_key=? AND runtime_family=? AND runtime_adapter_kind=?
AND runtime_version=? AND adapter_id=? AND entry_path=? AND size_bytes=? AND sha256=?
AND artifact_set_sha256=? AND requires_threads=?
AND save_payload_kind=? AND save_max_bytes=? AND compatibility_json=?
AND available_for_launch=?
`, artifact.CoreID, artifact.RouteKey, artifact.RuntimeFamily, artifact.RuntimeAdapterKind, artifact.RuntimeVersion,
		artifact.AdapterID, artifact.EntryPath, artifact.EntrySizeBytes, artifact.EntrySHA256,
		artifact.ArtifactSetSHA256, boolToInteger(artifact.RequiresThreads),
		artifact.SavePayloadKind, artifact.SaveMaxBytes, string(compatibility), available,
	).Scan(&id, &storedManifest, &storedProvenance); err != nil {
		return "", fmt.Errorf("verify immutable RPG Maker artifact: %w", err)
	}
	if !existing && (storedManifest != version.ManifestSHA256 || storedProvenance != string(provenance)) {
		return "", fmt.Errorf("%w: RPG Maker artifact provenance", ErrInvalid)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET selected_for_new_bindings=?,version=version+1,updated_at_ms=?
WHERE id=? AND selected_for_new_bindings<>?
`, selected, now.UnixMilli(), id, selected); err != nil {
		return "", fmt.Errorf("select RPG Maker artifact: %w", err)
	}
	return id, nil
}

func canonicalJSONObject(contents json.RawMessage) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(contents, &value); err != nil || value == nil {
		return nil, fmt.Errorf("%w: RPG Maker compatibility", ErrInvalid)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: RPG Maker compatibility", ErrInvalid)
	}
	return canonical, nil
}
