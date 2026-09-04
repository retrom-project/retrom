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
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	URL       string `json:"url"`
}

func (service *Service) ProjectIndex(
	ctx context.Context,
	launchID, capability string,
) (ProjectIndexView, error) {
	if index, err := service.productButterscotchProjectIndex(ctx, launchID, capability); err == nil {
		return index, nil
	}
	if index, err := service.productKiriKiriProjectIndex(ctx, launchID, capability); err == nil {
		return index, nil
	}
	if index, err := service.productONSProjectIndex(ctx, launchID, capability); err == nil {
		return index, nil
	}
	if index, err := service.reviewPreviewKiriKiriProjectIndex(ctx, launchID, capability); err == nil {
		return index, nil
	}
	if index, err := service.reviewPreviewButterscotchProjectIndex(ctx, launchID, capability); err == nil {
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
 game.title,launch.dependency_snapshot_json
FROM launch_sessions launch
JOIN games game ON game.id=launch.game_id
WHERE launch.id=? AND launch.purpose='PRODUCT'
 AND EXISTS(SELECT 1 FROM launch_content_files file WHERE file.launch_session_id=launch.id
  AND file.format_version='ONS_PROJECT')
`, launchID).Scan(&credentialHash, &state, &hardExpires, &title, &dependencyJSON)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		state != "ACTIVE" || hardExpires <= service.now().UnixMilli() {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil || len(title) > 500 {
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
SELECT content.logical_name,blob.size_bytes
FROM launch_content_files content
JOIN blobs blob ON blob.id=content.blob_id
WHERE content.launch_session_id=? AND content.format_version='ONS_PROJECT'
ORDER BY content.logical_name
`, launchID)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load product ONS project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]onsProjectIndexFile, 0)
	for rows.Next() {
		var file onsProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil || len(files) >= maximumONSProjectFiles {
			return ProjectIndexView{}, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, fmt.Errorf("read product ONS project index: %w", err)
	}
	return buildONSProjectIndex(projectRoot, title, profile, files)
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
WHERE id=? AND content_kind='ONS_PROJECT' AND content_format='ONS_PROJECT'
`, previewID).Scan(&credentialHash, &state, &hardExpires, &title, &dependencyJSON, &primaryName)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil || len(title) > 500 {
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
	files, err := service.reviewPreviewProjectIndexFiles(ctx, previewID, primaryName)
	if err != nil {
		return ProjectIndexView{}, err
	}
	return buildONSProjectIndex(projectRoot, title, profile, files)
}

func buildONSProjectIndex(
	projectRoot, title string,
	profile detector.Profile,
	files []onsProjectIndexFile,
) (ProjectIndexView, error) {
	if len(files) < 2 || len(files) > maximumONSProjectFiles {
		return ProjectIndexView{}, ErrCredential
	}
	seen := make(map[string]struct{}, len(files))
	markerFound, fontFound := false, false
	for index := range files {
		normalized, err := importing.ValidateLogicalPath(files[index].Path)
		folded := importing.ASCIICaseFold(normalized)
		if err != nil || normalized != files[index].Path || files[index].SizeBytes < 1 {
			return ProjectIndexView{}, ErrCredential
		}
		if _, duplicate := seen[folded]; duplicate {
			return ProjectIndexView{}, ErrCredential
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == profile.MarkerPath
		fontFound = fontFound || normalized == profile.FontPath
		files[index].URL = projectRoot + escapeProjectPath(normalized)
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
SELECT logical_name,size_bytes FROM (
 SELECT session.content_logical_name AS logical_name,blob.size_bytes,0 AS sort_order
 FROM review_preview_sessions session
 JOIN blobs blob ON blob.id=session.content_blob_id
 WHERE session.id=?
 UNION ALL
 SELECT file.logical_name,blob.size_bytes,file.sort_order+1
 FROM review_preview_files file
 JOIN blobs blob ON blob.id=file.blob_id
 WHERE file.preview_session_id=? AND file.role='PROJECT_FILE'
) ORDER BY sort_order,logical_name
`, previewID, previewID)
	if err != nil {
		return nil, fmt.Errorf("load review ONS project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]onsProjectIndexFile, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var file onsProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil || len(files) >= maximumONSProjectFiles {
			return nil, ErrCredential
		}
		normalized, pathErr := importing.ValidateLogicalPath(file.Path)
		folded := importing.ASCIICaseFold(normalized)
		if pathErr != nil || normalized != file.Path || file.SizeBytes < 1 {
			return nil, ErrCredential
		}
		if _, duplicate := seen[folded]; duplicate {
			return nil, ErrCredential
		}
		seen[folded] = struct{}{}
		files = append(files, file)
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
	var digest, state, format, coreID, providerID, targetID, bundleSHA256, platformKey string
	var hardExpires int64
	err = service.database.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.sha256,
preview.content_format,binding.core_id,preview.provider_id,preview.target_id,
preview.bundle_sha256,platform.id
FROM review_preview_sessions preview
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN (
 SELECT id AS preview_session_id,content_logical_name AS logical_name,content_blob_id AS blob_id
 FROM review_preview_sessions
 UNION ALL
 SELECT preview_session_id,logical_name,blob_id FROM review_preview_files WHERE role='PROJECT_FILE'
) file ON file.preview_session_id=preview.id AND file.logical_name=?
JOIN blobs blob ON blob.id=file.blob_id
WHERE preview.id=?
 AND preview.content_kind IN (
  'ONS_PROJECT','KIRIKIRI_PROJECT','BUTTERSCOTCH_PROJECT','TYRANOSCRIPT_PROJECT'
 )
`, normalized, previewID).Scan(
		&credentialHash, &state, &hardExpires, &digest, &format, &coreID,
		&providerID, &targetID, &bundleSHA256, &platformKey,
	)
	if err != nil || format != onsProjectFormat && format != kirikiriProjectFormat &&
		format != butterscotchProjectFormat && format != tyranoScriptProjectFormat ||
		!reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ContentView{}, ErrCredential
	}
	return ContentView{
		Digest: digest, Format: format, CoreID: coreID, ProviderID: providerID, TargetID: targetID,
		BundleSHA256: bundleSHA256, PlatformKey: platformKey,
	}, nil
}

func escapeProjectPath(logicalName string) string {
	parts := strings.Split(logicalName, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
