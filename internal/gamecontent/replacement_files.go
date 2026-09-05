package gamecontent

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentmanifest"
	"retrom/internal/contentprofile"
	"retrom/internal/multidisc"
	"retrom/internal/payloadrelease"
)

func collectUploadFiles(ctx context.Context, database *sql.DB, uploadID string) ([]uploadedFile, error) {
	rows, err := database.QueryContext(
		ctx,
		`
SELECT f.relative_path,
f.final_blob_id,
b.sha256,
b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.upload_session_id=?
AND f.state='COMPLETE'
ORDER BY f.relative_path,
f.id
`,
		uploadID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upload files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]uploadedFile, 0)
	for rows.Next() {
		var value uploadedFile
		if err := rows.Scan(&value.logicalName, &value.blobID, &value.sha256, &value.sizeBytes); err != nil {
			return nil, fmt.Errorf("scan upload file: %w", err)
		}
		files = append(files, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload files: %w", err)
	}
	return files, nil
}

func (service *Service) prepareReplacement(
	ctx context.Context,
	snapshot jobSnapshot,
	files []uploadedFile,
) (preparedReplacement, error) {
	if snapshot.ContentMode == contentcapability.ModeRPGMakerProject {
		return service.prepareRPGMakerReplacement(ctx, snapshot, files)
	}
	if snapshot.ContentMode != contentcapability.ModeMultiDisc {
		if len(files) == 0 || snapshot.PlatformID != "dos" && len(files) != 1 ||
			snapshot.PlatformID == "arcade" && !strings.EqualFold(filepath.Ext(files[0].logicalName), ".zip") {
			return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_GROUP_INVALID"}
		}
		replacement := preparedReplacement{contentKind: string(contentprofile.ContentKindSingleFile)}
		replacement.files = make([]replacementFile, 0, len(files))
		manifestFiles := make([]contentmanifest.File, 0, len(files))
		for index, file := range files {
			role := "COMPANION"
			if index == 0 {
				role = "CONTENT"
			}
			replacement.files = append(replacement.files, replacementFile{
				role: role, logicalName: file.logicalName, blobID: file.blobID,
				sha256: file.sha256, sizeBytes: file.sizeBytes, sortOrder: index,
			})
			manifestFiles = append(manifestFiles, contentmanifest.File{
				Role: role, LogicalName: file.logicalName, BlobSHA256: file.sha256, SizeBytes: file.sizeBytes,
			})
		}
		replacement.firstContentLogicalName = files[0].logicalName
		manifest, digest, err := contentmanifest.Build(replacement.contentKind, manifestFiles)
		if err != nil {
			return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
		}
		replacement.manifest, replacement.manifestDigest = manifest, digest
		return replacement, nil
	}
	return service.prepareMultiDiscReplacement(ctx, snapshot, files)
}

func (service *Service) prepareMultiDiscReplacement(
	ctx context.Context,
	snapshot jobSnapshot,
	files []uploadedFile,
) (preparedReplacement, error) {
	if err := ctx.Err(); err != nil {
		return preparedReplacement{}, fmt.Errorf("prepare multi-disc replacement: %w", err)
	}
	if service.blobs == nil || snapshot.MaxDiscs < multidisc.MinDiscs || snapshot.MaxDiscs > multidisc.MaxDiscs ||
		snapshot.MaxTotalBytes <= 0 {
		return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_VALIDATION_UNAVAILABLE"}
	}
	playlist, err := replacementPlaylist(files)
	if err != nil {
		return preparedReplacement{}, err
	}
	playlistBytes, err := service.readReplacementPlaylist(playlist)
	if err != nil {
		return preparedReplacement{}, err
	}
	directory := path.Dir(playlist.logicalName)
	candidates, err := service.replacementDiscCandidates(files, playlist.blobID, directory)
	if err != nil {
		return preparedReplacement{}, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: snapshot.MaxDiscs, MaxTotalBytes: snapshot.MaxTotalBytes,
	})
	if err != nil {
		var validationErr *multidisc.ValidationError
		if errors.As(err, &validationErr) {
			return preparedReplacement{}, &replacementValidationError{code: string(validationErr.Code)}
		}
		return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_INVALID"}
	}
	for _, entry := range parsed.Entries {
		if entry.State != multidisc.EntryPresent || entry.File == nil {
			return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_FILE_MISSING"}
		}
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_VALIDATION_UNAVAILABLE"}
	}
	return buildPreparedMultiDiscReplacement(playlist, parsed, canonical)
}

func replacementPlaylist(files []uploadedFile) (uploadedFile, error) {
	playlists := make([]uploadedFile, 0, 2)
	for _, file := range files {
		if strings.EqualFold(path.Ext(file.logicalName), ".m3u") {
			playlists = append(playlists, file)
		}
	}
	if len(playlists) == 0 {
		return uploadedFile{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_MISSING"}
	}
	if len(playlists) != 1 {
		return uploadedFile{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_AMBIGUOUS"}
	}
	return playlists[0], nil
}

func (service *Service) readReplacementPlaylist(playlist uploadedFile) ([]byte, error) {
	playlistFile, err := service.blobs.OpenDigest(playlist.sha256)
	if err != nil {
		return nil, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	defer func() { cleanup.Error("close", playlistFile.Close()) }()
	playlistBytes, err := io.ReadAll(io.LimitReader(playlistFile, multidisc.MaxPlaylistBytes+1))
	if err != nil {
		return nil, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	return playlistBytes, nil
}

func (service *Service) replacementDiscCandidates(
	files []uploadedFile,
	playlistBlobID, directory string,
) ([]multidisc.File, error) {
	candidates := make([]multidisc.File, 0, len(files))
	for _, file := range files {
		if file.blobID == playlistBlobID || path.Dir(file.logicalName) != directory ||
			!strings.EqualFold(path.Ext(file.logicalName), ".chd") {
			continue
		}
		candidate, err := service.replacementDiscCandidate(file)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (service *Service) replacementDiscCandidate(file uploadedFile) (multidisc.File, error) {
	blob, err := service.blobs.OpenDigest(file.sha256)
	if err != nil {
		return multidisc.File{}, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	defer func() { cleanup.Error("close", blob.Close()) }()
	header := make([]byte, 8)
	if _, err := io.ReadFull(blob, header); err != nil {
		return multidisc.File{}, &replacementValidationError{code: string(multidisc.CodeCHDInvalid)}
	}
	return multidisc.File{
		Basename: path.Base(file.logicalName), LogicalName: path.Base(file.logicalName),
		BlobID: file.blobID, BlobSHA256: file.sha256, SizeBytes: file.sizeBytes, Header: header,
	}, nil
}

func buildPreparedMultiDiscReplacement(
	playlist uploadedFile,
	parsed multidisc.Result,
	canonical blobstore.Metadata,
) (preparedReplacement, error) {
	replacement := preparedReplacement{
		contentKind: multidisc.ContentKind, canonicalPlaylist: canonical,
		files:                   make([]replacementFile, 0, len(parsed.Entries)+1),
		orderedDiscSHA256:       make([]string, 0, len(parsed.Entries)),
		firstContentLogicalName: parsed.Entries[0].File.LogicalName,
	}
	replacement.files = append(replacement.files, replacementFile{
		role: "PLAYLIST_SOURCE", logicalName: path.Base(playlist.logicalName), blobID: playlist.blobID,
		sha256: playlist.sha256, sizeBytes: playlist.sizeBytes, sortOrder: 0,
	})
	manifestFiles := make([]contentmanifest.File, 0, len(parsed.Entries)+1)
	manifestFiles = append(manifestFiles, contentmanifest.File{
		Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.logicalName),
		BlobSHA256: playlist.sha256, SizeBytes: playlist.sizeBytes,
	})
	for _, entry := range parsed.Entries {
		file := replacementFile{
			role: "DISC", logicalName: entry.File.LogicalName, blobID: entry.File.BlobID,
			sha256: entry.File.BlobSHA256, sizeBytes: entry.File.SizeBytes, sortOrder: entry.Ordinal,
		}
		replacement.files = append(replacement.files, file)
		replacement.orderedDiscSHA256 = append(replacement.orderedDiscSHA256, file.sha256)
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.sha256, SizeBytes: file.sizeBytes,
		})
	}
	manifest, manifestDigest, err := contentmanifest.Build(replacement.contentKind, manifestFiles)
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	replacement.manifest, replacement.manifestDigest = manifest, manifestDigest
	return replacement, nil
}

func (service *Service) fail(ctx context.Context, jobID, code string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=1,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'FAILED',
?,
?
FROM jobs
WHERE id=?
`,
		fmt.Sprintf(`{"code":%q}`, code),
		now,
		jobID,
	)
}

func (service *Service) failUnchanged(ctx context.Context, jobID string) {
	service.failTerminal(ctx, jobID, "GAME_CONTENT_UNCHANGED")
}

func (service *Service) failTerminal(ctx context.Context, jobID, code string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_DATABASE_FAILED")
		return
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, code, now, now, jobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'FAILED',?,?
FROM jobs WHERE id=? AND state='FAILED'
`, fmt.Sprintf(`{"code":%q}`, code), now, jobID); err != nil {
		return
	}
	var consumptionID string
	if err := transaction.QueryRowContext(ctx, `
SELECT id FROM upload_consumptions
WHERE consumer_type='GAME_CONTENT_REPLACE_JOB' AND consumer_id=?
`, jobID).Scan(&consumptionID); err != nil {
		return
	}
	if _, err := payloadrelease.ScheduleConsumption(ctx, transaction, consumptionID, now); err != nil {
		return
	}
	if err := transaction.Commit(); err != nil {
		return
	}
	if service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
}

func nullablePointer(value sql.NullString) *string {
	if value.Valid {
		return &value.String
	}
	return nil
}

func nullableText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func newID() string {
	value, _ := uuid.NewV7()
	return value.String()
}
