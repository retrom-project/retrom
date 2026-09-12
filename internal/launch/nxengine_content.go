package launch

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/nxengine/detector"
)

const maximumNXEngineProjectFiles = 4096

func (service *Service) reviewPreviewNXEngineContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return service.reviewPreviewProjectContent(
		ctx, source, profile.MarkerPath, nxengineProjectFormat,
		"NXEngine",
	)
}

type nxengineProjectIndex = runtimeProjectIndex

type nxengineProjectIndexFile = runtimeProjectIndexFile

type nxengineIndexSource struct {
	credentialSQL string
	filesSQL      string
	filesArgs     []any
}

func (service *Service) productNXEngineProjectIndex(
	ctx context.Context, id, capability string,
) (ProjectIndexView, error) {
	return service.nxengineProjectIndex(ctx, id, capability, nxengineIndexSource{
		credentialSQL: `SELECT credential_sha256,state,hard_expires_at_ms,dependency_snapshot_json
FROM launch_sessions WHERE id=? AND EXISTS(
 SELECT 1 FROM launch_content_files WHERE launch_session_id=launch_sessions.id AND format_version='NXENGINE_PROJECT')`,
		filesSQL: `SELECT file.logical_name,blob.size_bytes FROM launch_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.format_version='NXENGINE_PROJECT' ORDER BY file.logical_name`,
		filesArgs: []any{id},
	})
}

func (service *Service) reviewPreviewNXEngineProjectIndex(
	ctx context.Context, id, capability string,
) (ProjectIndexView, error) {
	return service.nxengineProjectIndex(ctx, id, capability, nxengineIndexSource{
		credentialSQL: `SELECT credential_sha256,state,hard_expires_at_ms,dependency_snapshot_json
FROM review_preview_sessions WHERE id=? AND content_kind='NXENGINE_PROJECT' AND content_format='NXENGINE_PROJECT'`,
		filesSQL: `SELECT logical_name,size_bytes FROM (
 SELECT preview.content_logical_name AS logical_name,blob.size_bytes AS size_bytes,0 AS sort_order
 FROM review_preview_sessions preview JOIN blobs blob ON blob.id=preview.content_blob_id WHERE preview.id=?
 UNION ALL
 SELECT file.logical_name,blob.size_bytes,file.sort_order+1 FROM review_preview_files file
 JOIN blobs blob ON blob.id=file.blob_id WHERE file.preview_session_id=? AND file.role='PROJECT_FILE'
) ORDER BY sort_order,logical_name`,
		filesArgs: []any{id, id},
	})
}

func (service *Service) nxengineProjectIndex(
	ctx context.Context, id, capability string, source nxengineIndexSource,
) (ProjectIndexView, error) {
	var hash []byte
	var state, snapshot string
	var expires int64
	err := service.database.QueryRowContext(ctx, source.credentialSQL, id).Scan(&hash, &state, &expires, &snapshot)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, hash, state, expires) {
		return ProjectIndexView{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(snapshot)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	root, err := service.runtimeProjectRoot(ctx, id, capability)
	if err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, source.filesSQL, source.filesArgs...)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("load NXEngine project index: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files, err := readNXEngineProjectIndexFiles(rows)
	if err != nil {
		return ProjectIndexView{}, err
	}
	if err := rows.Err(); err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	return buildNXEngineProjectIndex(root, profile, files)
}

func readNXEngineProjectIndexFiles(rows rowScanner) ([]nxengineProjectIndexFile, error) {
	files := make([]nxengineProjectIndexFile, 0)
	for rows.Next() {
		var file nxengineProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil ||
			len(files) >= maximumNXEngineProjectFiles {
			return nil, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrCredential
	}
	return files, nil
}

func buildNXEngineProjectIndex(
	projectRoot string,
	profile detector.Profile,
	files []nxengineProjectIndexFile,
) (ProjectIndexView, error) {
	return buildRuntimeProjectIndex(
		projectRoot, profile.MarkerPath, files, maximumNXEngineProjectFiles, false, "NXEngine",
	)
}
