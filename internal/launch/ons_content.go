package launch

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/ons/detector"
)

func (service *Service) buildONSProductContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	if selection.contentKind != onsProjectFormat {
		return launchContentPlan{}, ErrBlocked
	}
	var dependencyJSON, compatibilityJSON, adapterKind, adapterID string
	if err := service.database.QueryRowContext(ctx, `
SELECT revision.dependency_snapshot_json,artifact.compatibility_json,
 artifact.runtime_adapter_kind,artifact.adapter_id
FROM game_variant_revisions revision
JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
JOIN core_artifacts artifact ON artifact.id=?
WHERE revision.id=? AND revision.game_content_revision_id=?
 AND artifact.runtime_family='ONS' AND artifact.available_for_launch=1
 AND artifact.core_id=bound_artifact.core_id AND artifact.route_key=bound_artifact.route_key
 AND json_extract(artifact.compatibility_json,'$.gameCompatibilityLine')=
     json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
`, selection.artifactID, selection.variantRevisionID, selection.contentRevisionID).Scan(
		&dependencyJSON, &compatibilityJSON, &adapterKind, &adapterID,
	); err != nil {
		return launchContentPlan{}, ErrBlocked
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil || adapterKind != "ONS_YURI_WEB" || adapterID != "ons-yuri-web" {
		return launchContentPlan{}, ErrBlocked
	}
	compatibility, err := parseONSCompatibility(compatibilityJSON)
	if err != nil {
		return launchContentPlan{}, ErrBlocked
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(
		selection.runtimeVersion, compatibility.JSPath,
	); !exists {
		return launchContentPlan{}, ErrBlocked
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(
		selection.runtimeVersion, compatibility.WasmPath,
	); !exists {
		return launchContentPlan{}, ErrBlocked
	}
	files, err := service.onsProductFiles(ctx, selection.contentRevisionID)
	if err != nil || !validONSLockedFiles(files, profile) {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: onsProjectFormat, Files: files}, nil
}

func (service *Service) onsProductFiles(
	ctx context.Context,
	contentRevisionID string,
) ([]lockedContentFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT blob_id,logical_name FROM game_content_files
WHERE game_content_revision_id=? AND role='PROJECT_FILE'
ORDER BY sort_order,logical_name
`, contentRevisionID)
	if err != nil {
		return nil, fmt.Errorf("load ONS product files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]lockedContentFile, 0)
	for rows.Next() {
		var file lockedContentFile
		file.Format = onsProjectFormat
		if err := rows.Scan(&file.BlobID, &file.LogicalName); err != nil ||
			len(files) >= maximumONSProjectFiles {
			return nil, ErrBlocked
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read ONS product files: %w", err)
	}
	return files, nil
}

func validONSLockedFiles(files []lockedContentFile, profile detector.Profile) bool {
	if len(files) < 2 || len(files) > maximumONSProjectFiles {
		return false
	}
	seen := make(map[string]struct{}, len(files))
	markerFound, fontFound := false, false
	for _, file := range files {
		normalized, err := importing.ValidateLogicalPath(file.LogicalName)
		if err != nil || normalized != file.LogicalName || file.BlobID == "" || file.Format != onsProjectFormat {
			return false
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return false
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == profile.MarkerPath
		fontFound = fontFound || normalized == profile.FontPath
	}
	return markerFound && fontFound
}
