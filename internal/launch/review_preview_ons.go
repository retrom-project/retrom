package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/ons/detector"
	retromruntime "retrom/internal/runtime"
)

const maximumONSProjectFiles = 100_000

func (service *Service) validateONSReviewPreviewSource(source reviewPreviewSource) error {
	if _, err := detector.ParseSnapshot(source.DependencySnapshot); err != nil {
		return ErrReviewPreviewUnavailable
	}
	compatibility, err := parseONSCompatibility(source.CompatibilityJSON)
	if err != nil || source.AdapterKind != "ONS_YURI_WEB" || source.AdapterID != "ons-yuri-web" ||
		source.CoreID != "onscripter_yuri" || source.ContentKind != onsProjectFormat {
		return ErrReviewPreviewUnavailable
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(
		source.RuntimeVersion, compatibility.JSPath,
	); !exists {
		return ErrReviewPreviewUnavailable
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(
		source.RuntimeVersion, compatibility.WasmPath,
	); !exists {
		return ErrReviewPreviewUnavailable
	}
	return nil
}

type ProjectIndexView struct {
	Contents []byte
	SHA256   string
}

type onsProjectIndex struct {
	Files         []onsProjectIndexFile `json:"files"`
	FontPath      string                `json:"fontPath"`
	SchemaVersion int                   `json:"schemaVersion"`
	Title         string                `json:"title"`
}

type onsProjectIndexFile struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

func (service *Service) ProjectIndex(
	ctx context.Context,
	launchID, capability string,
) (ProjectIndexView, error) {
	if index, err := service.productONSProjectIndex(ctx, launchID, capability); err == nil {
		return index, nil
	}
	return service.ReviewPreviewProjectIndex(ctx, launchID, capability)
}

func (service *Service) productONSProjectIndex(
	ctx context.Context,
	launchID, capability string,
) (ProjectIndexView, error) {
	var credentialHash []byte
	var state, title, dependencyJSON string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.hard_expires_at_ms,
 metadata.title,revision.dependency_snapshot_json
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
JOIN games game ON game.id=launch.game_id
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
WHERE launch.id=? AND launch.purpose='PRODUCT' AND artifact.runtime_family='ONS'
 AND artifact.available_for_launch=1
`, launchID).Scan(&credentialHash, &state, &hardExpires, &title, &dependencyJSON)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		state != "ACTIVE" || hardExpires <= service.now().UnixMilli() {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil || len(title) > 500 {
		return ProjectIndexView{}, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name FROM launch_content_files
WHERE launch_session_id=? AND format_version='ONS_PROJECT_V1'
ORDER BY logical_name
`, launchID)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load product ONS project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil || len(paths) >= maximumONSProjectFiles {
			return ProjectIndexView{}, ErrCredential
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, fmt.Errorf("read product ONS project index: %w", err)
	}
	return buildONSProjectIndex(launchID, title, profile, paths)
}

func (service *Service) reviewPreviewONSContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	var content reviewPreviewContentSet
	content.Format = onsProjectFormat
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
		return reviewPreviewContentSet{}, fmt.Errorf("review ONS project files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	content.Files = make([]reviewPreviewFile, 0)
	for rows.Next() {
		var file reviewPreviewFile
		file.Role = "PROJECT_FILE"
		if err := rows.Scan(&file.LogicalName, &file.BlobID, &file.SortOrder); err != nil ||
			len(content.Files) >= maximumONSProjectFiles {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
		content.Files = append(content.Files, file)
	}
	if err := rows.Err(); err != nil || len(content.Files) == 0 {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return content, nil
}

func (service *Service) ReviewPreviewProjectIndex(
	ctx context.Context,
	previewID, capability string,
) (ProjectIndexView, error) {
	var credentialHash []byte
	var state, title, dependencyJSON, primaryName string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms,title,dependency_snapshot_json,content_logical_name
FROM review_preview_sessions
WHERE id=? AND content_kind='ONS_PROJECT_V1' AND content_format='ONS_PROJECT_V1'
`, previewID).Scan(&credentialHash, &state, &hardExpires, &title, &dependencyJSON, &primaryName)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil || len(title) > 500 {
		return ProjectIndexView{}, ErrCredential
	}
	files, err := service.reviewPreviewProjectIndexFiles(ctx, previewID, primaryName)
	if err != nil {
		return ProjectIndexView{}, err
	}
	paths := make([]string, len(files))
	for index, file := range files {
		paths[index] = file.Path
	}
	return buildONSProjectIndex(previewID, title, profile, paths)
}

func buildONSProjectIndex(
	launchID, title string,
	profile detector.Profile,
	paths []string,
) (ProjectIndexView, error) {
	if len(paths) < 2 || len(paths) > maximumONSProjectFiles {
		return ProjectIndexView{}, ErrCredential
	}
	files := make([]onsProjectIndexFile, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	markerFound, fontFound := false, false
	for _, logicalName := range paths {
		normalized, err := importing.ValidateLogicalPath(logicalName)
		folded := importing.ASCIICaseFold(normalized)
		if err != nil || normalized != logicalName {
			return ProjectIndexView{}, ErrCredential
		}
		if _, duplicate := seen[folded]; duplicate {
			return ProjectIndexView{}, ErrCredential
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == profile.MarkerPath
		fontFound = fontFound || normalized == profile.FontPath
		files = append(files, onsProjectIndexFile{
			Path: normalized, URL: "/runtime/projects/" + launchID + "/" + escapeProjectPath(normalized),
		})
	}
	if !markerFound || !fontFound {
		return ProjectIndexView{}, ErrCredential
	}
	contents, err := json.Marshal(onsProjectIndex{
		Files: files, FontPath: profile.FontPath, SchemaVersion: 1, Title: title,
	})
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("marshal ONS project index: %w", err)
	}
	digest := sha256.Sum256(contents)
	return ProjectIndexView{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}

func (service *Service) reviewPreviewProjectIndexFiles(
	ctx context.Context,
	previewID, primaryName string,
) ([]onsProjectIndexFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name FROM (
 SELECT content_logical_name AS logical_name,0 AS sort_order FROM review_preview_sessions WHERE id=?
 UNION ALL
 SELECT logical_name,sort_order+1 FROM review_preview_files
 WHERE preview_session_id=? AND role='PROJECT_FILE'
) ORDER BY sort_order,logical_name
`, previewID, previewID)
	if err != nil {
		return nil, fmt.Errorf("load review ONS project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]onsProjectIndexFile, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var logicalName string
		if err := rows.Scan(&logicalName); err != nil || len(files) >= maximumONSProjectFiles {
			return nil, ErrCredential
		}
		normalized, pathErr := importing.ValidateLogicalPath(logicalName)
		folded := importing.ASCIICaseFold(normalized)
		if pathErr != nil {
			return nil, ErrCredential
		}
		if _, duplicate := seen[folded]; duplicate {
			return nil, ErrCredential
		}
		seen[folded] = struct{}{}
		files = append(files, onsProjectIndexFile{
			Path: normalized, URL: "/runtime/projects/" + previewID + "/" + escapeProjectPath(normalized),
		})
	}
	if err := rows.Err(); err != nil || len(files) < 2 || files[0].Path != primaryName {
		return nil, ErrCredential
	}
	return files, nil
}

func (service *Service) ReviewPreviewProjectContent(
	ctx context.Context,
	previewID, capability, logicalName string,
) (ContentView, error) {
	normalized, err := importing.ValidateLogicalPath(logicalName)
	if err != nil || normalized == "index.json" {
		return ContentView{}, ErrCredential
	}
	var credentialHash []byte
	var digest, state, format, coreID, platformKey string
	var hardExpires, artifactVersion int64
	err = service.database.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.sha256,
preview.content_format,artifact.core_id,artifact.version,platform.id
FROM review_preview_sessions preview
JOIN core_artifacts artifact ON artifact.id=preview.core_artifact_id AND artifact.runtime_family='ONS'
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN (
 SELECT id AS preview_session_id,content_logical_name AS logical_name,content_blob_id AS blob_id
 FROM review_preview_sessions
 UNION ALL
 SELECT preview_session_id,logical_name,blob_id FROM review_preview_files WHERE role='PROJECT_FILE'
) file ON file.preview_session_id=preview.id AND file.logical_name=?
JOIN blobs blob ON blob.id=file.blob_id
WHERE preview.id=? AND preview.content_kind='ONS_PROJECT_V1'
`, normalized, previewID).Scan(
		&credentialHash, &state, &hardExpires, &digest, &format, &coreID, &artifactVersion, &platformKey,
	)
	if err != nil || format != onsProjectFormat ||
		!reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ContentView{}, ErrCredential
	}
	return ContentView{
		Digest: digest, Format: format, CoreID: coreID,
		PlatformKey: platformKey, ArtifactVersion: artifactVersion,
	}, nil
}

func escapeProjectPath(logicalName string) string {
	parts := strings.Split(logicalName, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
