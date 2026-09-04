package launch

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/kirikiri/detector"
	retromruntime "retrom/internal/runtime"
)

const maximumKiriKiriProjectFiles = 10_000

func (service *Service) reviewPreviewKiriKiriContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return service.reviewPreviewProjectContent(
		ctx, source, profile.MarkerPath, kirikiriProjectFormat, maximumKiriKiriProjectFiles, "KiriKiri",
	)
}

type kirikiriProjectIndex = runtimeProjectIndex

type kirikiriProjectIndexFile = runtimeProjectIndexFile

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
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE launch.id=? AND launch.purpose='PRODUCT'
 AND launch.provider_id=revision.provider_id AND launch.target_id=revision.target_id
 AND EXISTS(SELECT 1 FROM launch_content_files file WHERE file.launch_session_id=launch.id
  AND file.format_version='KIRIKIRI_PROJECT')
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
WHERE file.launch_session_id=? AND file.format_version='KIRIKIRI_PROJECT'
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
WHERE id=? AND content_kind='KIRIKIRI_PROJECT' AND content_format='KIRIKIRI_PROJECT'
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
	return buildRuntimeProjectIndex(
		projectRoot, profile.MarkerPath, files, maximumKiriKiriProjectFiles, true, "KiriKiri",
	)
}
