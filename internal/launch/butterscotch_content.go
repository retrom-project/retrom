package launch

import (
	"context"
	"fmt"

	"retrom/internal/butterscotch/detector"
	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
)

const maximumButterscotchProjectFiles = 10_000

func (service *Service) reviewPreviewButterscotchContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return service.reviewPreviewProjectContent(
		ctx, source, profile.MarkerPath, butterscotchProjectFormat, maximumButterscotchProjectFiles,
		"Butterscotch",
	)
}

type butterscotchProjectIndex = runtimeProjectIndex

type butterscotchProjectIndexFile = runtimeProjectIndexFile

func (service *Service) productButterscotchProjectIndex(
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
  AND file.format_version='BUTTERSCOTCH_PROJECT_V1')
`, launchID).Scan(&credentialHash, &state, &hardExpires, &dependencyJSON)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		state != "ACTIVE" || hardExpires <= service.now().UnixMilli() {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(dependencyJSON)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	projectRoot, err := service.runtimeProjectRoot(ctx, launchID, capability)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,blob.size_bytes FROM launch_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.format_version='BUTTERSCOTCH_PROJECT_V1'
ORDER BY file.logical_name
`, launchID)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load product Butterscotch project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files, err := readButterscotchProjectIndexFiles(rows)
	if err != nil {
		return ProjectIndexView{}, err
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	return buildButterscotchProjectIndex(projectRoot, profile, files)
}

func (service *Service) reviewPreviewButterscotchProjectIndex(
	ctx context.Context,
	previewID, capability string,
) (ProjectIndexView, error) {
	var credentialHash []byte
	var state, dependencyJSON string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms,dependency_snapshot_json
FROM review_preview_sessions
WHERE id=? AND content_kind='BUTTERSCOTCH_PROJECT_V1' AND content_format='BUTTERSCOTCH_PROJECT_V1'
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
	projectRoot, err := service.runtimeProjectRoot(ctx, previewID, capability)
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
		return ProjectIndexView{}, fmt.Errorf("load review Butterscotch project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files, err := readButterscotchProjectIndexFiles(rows)
	if err != nil {
		return ProjectIndexView{}, err
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	return buildButterscotchProjectIndex(projectRoot, profile, files)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func readButterscotchProjectIndexFiles(rows rowScanner) ([]butterscotchProjectIndexFile, error) {
	files := make([]butterscotchProjectIndexFile, 0)
	for rows.Next() {
		var file butterscotchProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil ||
			len(files) >= maximumButterscotchProjectFiles {
			return nil, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrCredential
	}
	return files, nil
}

func (service *Service) runtimeProjectRoot(
	ctx context.Context,
	id, capability string,
) (string, error) {
	identity, err := service.ProjectContentIdentity(ctx, id, capability)
	if err != nil {
		return "", err
	}
	return RuntimeProjectContentRoot(identity)
}

func buildButterscotchProjectIndex(
	projectRoot string,
	profile detector.Profile,
	files []butterscotchProjectIndexFile,
) (ProjectIndexView, error) {
	return buildRuntimeProjectIndex(
		projectRoot, profile.MarkerPath, files, maximumButterscotchProjectFiles, false, "Butterscotch",
	)
}
