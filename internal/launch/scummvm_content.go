package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/scummvm"
)

func (service *Service) reviewPreviewScummVMContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	snapshot, err := scummvm.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	if _, err := snapshot.Selected(); err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	// A primary blob anchors the existing preview file set. It does not select a game.
	var first string
	err = service.database.QueryRowContext(ctx, `
SELECT logical_name
FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='PROJECT_FILE'
ORDER BY logical_name LIMIT 1
`, source.SourceSnapshotID).Scan(&first)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return service.reviewPreviewProjectContent(ctx, source, first, scummvm.ContentKind, "ScummVM")
}

func (service *Service) scummVMProjectIndex(
	ctx context.Context,
	sessionID,
	capability string,
) (ProjectIndexView, error) {
	preview, snapshot, err := service.authorizeScummVMIndex(ctx, sessionID, capability)
	if err != nil {
		return ProjectIndexView{}, err
	}
	if _, err := snapshot.Selected(); err != nil {
		return ProjectIndexView{}, ErrCredential
	}
	identity, err := service.ProjectContentIdentity(ctx, sessionID, capability)
	if err != nil {
		return ProjectIndexView{}, err
	}
	root, err := RuntimeProjectContentRoot(identity)
	if err != nil {
		return ProjectIndexView{}, err
	}
	files, err := service.scummVMIndexFiles(ctx, sessionID, preview)
	if err != nil || len(files) == 0 {
		return ProjectIndexView{}, ErrCredential
	}
	return buildRuntimeProjectIndex(root, files[0].Path, files, 10_000, true, "ScummVM")
}

func (service *Service) authorizeScummVMIndex(
	ctx context.Context,
	id,
	capability string,
) (bool, scummvm.Snapshot, error) {
	var credential []byte
	var state, raw string
	var expires int64
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.hard_expires_at_ms,launch.dependency_snapshot_json
FROM launch_sessions launch
WHERE launch.id=?
AND EXISTS(SELECT 1
FROM launch_content_files file
WHERE file.launch_session_id=launch.id
AND file.format_version='SCUMMVM_PROJECT')
`, id).Scan(&credential, &state, &expires, &raw)
	preview := errors.Is(err, sql.ErrNoRows)
	if preview {
		err = service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms,dependency_snapshot_json
FROM review_preview_sessions
WHERE id=? AND content_kind='SCUMMVM_PROJECT' AND content_format='SCUMMVM_PROJECT'
`, id).Scan(&credential, &state, &expires, &raw)
	}
	if err != nil || state != "ACTIVE" || expires <= service.now().UnixMilli() ||
		!retromruntime.MatchesCapability(capability, credential) {
		return false, scummvm.Snapshot{}, ErrCredential
	}
	snapshot, err := scummvm.ParseSnapshot(raw)
	if err != nil {
		return false, scummvm.Snapshot{}, ErrCredential
	}
	return preview, snapshot, nil
}

func (service *Service) scummVMIndexFiles(
	ctx context.Context,
	id string,
	preview bool,
) ([]runtimeProjectIndexFile, error) {
	query := `
SELECT file.logical_name,blob.size_bytes
FROM launch_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=?
AND file.format_version='SCUMMVM_PROJECT'
ORDER BY file.logical_name
`
	arguments := []any{id}
	if preview {
		query = `
SELECT logical_name,size_bytes
FROM (
SELECT session.content_logical_name AS logical_name,blob.size_bytes
FROM review_preview_sessions session
JOIN blobs blob ON blob.id=session.content_blob_id
WHERE session.id=?
UNION ALL SELECT file.logical_name,blob.size_bytes
FROM review_preview_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.preview_session_id=? AND file.role='PROJECT_FILE')
ORDER BY logical_name
`
		arguments = append(arguments, id)
	}
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("read ScummVM project index: %w", err)
	}
	defer func() { cleanup.Error("close ScummVM index", rows.Close()) }()
	files := make([]runtimeProjectIndexFile, 0)
	for rows.Next() {
		var file runtimeProjectIndexFile
		if err := rows.Scan(&file.Path, &file.SizeBytes); err != nil || len(files) >= 10_000 {
			return nil, ErrCredential
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read ScummVM index: %w", err)
	}
	return files, nil
}
