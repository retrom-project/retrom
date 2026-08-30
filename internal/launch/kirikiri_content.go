package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/kirikiri/detector"
	retromruntime "retrom/internal/runtime"
)

const maximumKiriKiriProjectFiles = 10_000

func (service *Service) buildKiriKiriProductContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	if selection.contentKind != kirikiriProjectFormat {
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
 AND artifact.runtime_family='KIRIKIRI' AND artifact.available_for_launch=1
 AND artifact.core_id=bound_artifact.core_id AND artifact.route_key=bound_artifact.route_key
 AND json_extract(artifact.compatibility_json,'$.gameCompatibilityLine')=
     json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
`, selection.artifactID, selection.variantRevisionID, selection.contentRevisionID).Scan(
		&dependencyJSON, &compatibilityJSON, &adapterKind, &adapterID,
	); err != nil {
		return launchContentPlan{}, ErrBlocked
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil || adapterKind != "KIRIKIRI2_WEB" || adapterID != "kirikiri2-web" {
		return launchContentPlan{}, ErrBlocked
	}
	compatibility, err := parseKiriKiriCompatibility(compatibilityJSON)
	if err != nil || !service.kiriKiriRuntimeFilesAvailable(selection.runtimeVersion, compatibility) {
		return launchContentPlan{}, ErrBlocked
	}
	files, err := service.projectProductFiles(
		ctx, selection.contentRevisionID, kirikiriProjectFormat, maximumKiriKiriProjectFiles,
	)
	if err != nil || !validKiriKiriLockedFiles(files, profile) {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: kirikiriProjectFormat, Files: files}, nil
}

func validKiriKiriLockedFiles(files []lockedContentFile, profile detector.Profile) bool {
	if len(files) < 1 || len(files) > maximumKiriKiriProjectFiles {
		return false
	}
	seen := make(map[string]struct{}, len(files))
	markerFound := false
	for _, file := range files {
		normalized, err := importing.ValidateLogicalPath(file.LogicalName)
		if err != nil || normalized != file.LogicalName || file.BlobID == "" ||
			file.Format != kirikiriProjectFormat {
			return false
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return false
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == profile.MarkerPath
	}
	return markerFound
}

func (service *Service) validateKiriKiriReviewPreviewSource(source reviewPreviewSource) error {
	if _, err := detector.ParseSnapshot(source.DependencySnapshot); err != nil {
		return ErrReviewPreviewUnavailable
	}
	compatibility, err := parseKiriKiriCompatibility(source.CompatibilityJSON)
	if err != nil || source.AdapterKind != "KIRIKIRI2_WEB" || source.AdapterID != "kirikiri2-web" ||
		source.CoreID != "kirikiri2" || source.ContentKind != kirikiriProjectFormat ||
		!service.kiriKiriRuntimeFilesAvailable(source.RuntimeVersion, compatibility) {
		return ErrReviewPreviewUnavailable
	}
	return nil
}

func (service *Service) reviewPreviewKiriKiriContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	var content reviewPreviewContentSet
	content.Format = kirikiriProjectFormat
	if err := service.database.QueryRowContext(ctx, `
SELECT blob_id,logical_name FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='PROJECT_FILE' AND logical_name=?
`, source.SourceSnapshotID, profile.MarkerPath).Scan(&content.BlobID, &content.LogicalName); err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name,blob_id,sort_order FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='PROJECT_FILE' AND logical_name<>?
ORDER BY sort_order,logical_name
`, source.SourceSnapshotID, profile.MarkerPath)
	if err != nil {
		return reviewPreviewContentSet{}, fmt.Errorf("review KiriKiri project files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	content.Files = make([]reviewPreviewFile, 0)
	for rows.Next() {
		var file reviewPreviewFile
		file.Role = "PROJECT_FILE"
		if err := rows.Scan(&file.LogicalName, &file.BlobID, &file.SortOrder); err != nil ||
			len(content.Files) >= maximumKiriKiriProjectFiles {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
		content.Files = append(content.Files, file)
	}
	if err := rows.Err(); err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return content, nil
}

type kirikiriProjectIndex struct {
	Files         []kirikiriProjectIndexFile `json:"files"`
	SchemaVersion int                        `json:"schemaVersion"`
}

type kirikiriProjectIndexFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	URL       string `json:"url"`
}

func (service *Service) productKiriKiriProjectIndex(
	ctx context.Context,
	launchID, capability string,
) (ProjectIndexView, error) {
	var credentialHash []byte
	var state, dependencyJSON string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.hard_expires_at_ms,
 revision.dependency_snapshot_json
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE launch.id=? AND launch.purpose='PRODUCT' AND artifact.runtime_family='KIRIKIRI'
 AND artifact.available_for_launch=1
`, launchID).Scan(&credentialHash, &state, &hardExpires, &dependencyJSON)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		state != "ACTIVE" || hardExpires <= service.now().UnixMilli() {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	identity, err := service.ProjectContentIdentity(ctx, launchID, capability)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	projectRoot, err := RuntimeProjectContentRoot(identity)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,blob.size_bytes FROM launch_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.format_version='KIRIKIRI_PROJECT_V1'
ORDER BY file.logical_name
`, launchID)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load product KiriKiri project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]kirikiriProjectIndexFile, 0)
	for rows.Next() {
		var file kirikiriProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil ||
			len(files) >= maximumKiriKiriProjectFiles {
			return ProjectIndexView{}, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, fmt.Errorf("read product KiriKiri project index: %w", err)
	}
	return buildKiriKiriProjectIndex(projectRoot, profile, files)
}

func (service *Service) reviewPreviewKiriKiriProjectIndex(
	ctx context.Context,
	previewID, capability string,
) (ProjectIndexView, error) {
	var credentialHash []byte
	var state, dependencyJSON string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms,dependency_snapshot_json
FROM review_preview_sessions
WHERE id=? AND content_kind='KIRIKIRI_PROJECT_V1' AND content_format='KIRIKIRI_PROJECT_V1'
`, previewID).Scan(&credentialHash, &state, &hardExpires, &dependencyJSON)
	if err != nil || !reviewPreviewCredential(
		service.now().UnixMilli(), capability, credentialHash, state, hardExpires,
	) {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	identity, err := service.ProjectContentIdentity(ctx, previewID, capability)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	projectRoot, err := RuntimeProjectContentRoot(identity)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name,size_bytes FROM (
 SELECT preview.content_logical_name AS logical_name,blob.size_bytes AS size_bytes,0 AS sort_order
 FROM review_preview_sessions preview JOIN blobs blob ON blob.id=preview.content_blob_id WHERE preview.id=?
 UNION ALL
 SELECT file.logical_name,blob.size_bytes,file.sort_order+1 FROM review_preview_files file
 JOIN blobs blob ON blob.id=file.blob_id
 WHERE file.preview_session_id=? AND file.role='PROJECT_FILE'
) ORDER BY sort_order,logical_name
`, previewID, previewID)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load review KiriKiri project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]kirikiriProjectIndexFile, 0)
	for rows.Next() {
		var file kirikiriProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil ||
			len(files) >= maximumKiriKiriProjectFiles {
			return ProjectIndexView{}, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, fmt.Errorf("read review KiriKiri project index: %w", err)
	}
	return buildKiriKiriProjectIndex(projectRoot, profile, files)
}

func buildKiriKiriProjectIndex(
	projectRoot string,
	profile detector.Profile,
	files []kirikiriProjectIndexFile,
) (ProjectIndexView, error) {
	if len(files) < 1 || len(files) > maximumKiriKiriProjectFiles {
		return ProjectIndexView{}, ErrCredential
	}
	seen := make(map[string]struct{}, len(files))
	markerFound := false
	for index := range files {
		normalized, err := importing.ValidateLogicalPath(files[index].Path)
		if err != nil || normalized != files[index].Path || files[index].SizeBytes < 0 {
			return ProjectIndexView{}, ErrCredential
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return ProjectIndexView{}, ErrCredential
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == profile.MarkerPath
		files[index].URL = projectRoot + escapeProjectPath(normalized)
	}
	if !markerFound {
		return ProjectIndexView{}, ErrCredential
	}
	contents, err := json.Marshal(kirikiriProjectIndex{Files: files, SchemaVersion: 1})
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("marshal KiriKiri project index: %w", err)
	}
	digest := sha256.Sum256(contents)
	return ProjectIndexView{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}
