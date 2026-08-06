package uploads

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/importing"
)

const PartSize = int64(8 << 20)

var ErrInvalid = errors.New("UPLOAD_INVALID")

type Canceled struct {
	UploadID string `json:"uploadId"`
	State    string `json:"state"`
	Version  int64  `json:"version"`
}

type FileDeclaration struct {
	ClientFileID string `json:"clientFileId"`
	RelativePath string `json:"relativePath"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type CreateRequest struct {
	SourceType string            `json:"sourceType"`
	Files      []FileDeclaration `json:"files"`
}

type File struct {
	ID           string `json:"fileId"`
	ClientFileID string `json:"clientFileId,omitempty"`
	RelativePath string `json:"relativePath"`
	SizeBytes    int64  `json:"sizeBytes"`
	Received     int64  `json:"receivedSizeBytes"`
	State        string `json:"state"`
	Parts        []int  `json:"receivedParts"`
}

type Session struct {
	ID             string `json:"uploadId"`
	State          string `json:"state"`
	SourceType     string `json:"sourceType"`
	TotalBytes     int64  `json:"totalBytes"`
	FinalizationNo int64  `json:"finalizationNo"`
	FinalizeJobID  any    `json:"finalizeJobId"`
	Version        int64  `json:"version"`
	ExpiresAtMS    int64  `json:"expiresAtMs"`
	ChunkSizeBytes int64  `json:"chunkSizeBytes"`
	Files          []File `json:"files"`
}

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	dataDir  string
	now      func() time.Time
}

func New(database *sql.DB, blobs *blobstore.Store, dataDir string, now func() time.Time) *Service {
	return &Service{database: database, blobs: blobs, dataDir: dataDir, now: now}
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Session, error) {
	if request.SourceType != "FILES" && request.SourceType != "DIRECTORY" || len(request.Files) < 1 ||
		len(request.Files) > 10_000 {
		return Session{}, ErrInvalid
	}
	seen := make(map[string]struct{}, len(request.Files))
	var total int64
	for _, file := range request.Files {
		if file.ClientFileID == "" || file.SizeBytes < 0 || file.SizeBytes > 8<<30 ||
			len([]byte(file.RelativePath)) > 1024 {
			return Session{}, ErrInvalid
		}
		if _, err := importing.ValidateLogicalPath(file.RelativePath); err != nil {
			return Session{}, ErrInvalid
		}
		if _, exists := seen[file.RelativePath]; exists {
			return Session{}, ErrInvalid
		}
		seen[file.RelativePath] = struct{}{}
		total += file.SizeBytes
		if total > 32<<30 {
			return Session{}, ErrInvalid
		}
	}
	manifest, _ := json.Marshal(request)
	digest := sha256.Sum256(manifest)
	uploadID, err := uuid.NewV7()
	if err != nil {
		return Session{}, fmt.Errorf("generate upload id: %w", err)
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin upload: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_sessions(id,
state,
source_type,
total_files,
total_bytes,
manifest_digest,
expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'CREATED',
?,
?,
?,
?,
?,
?,
?)
`,
		uploadID.String(),
		request.SourceType,
		len(request.Files),
		total,
		hex.EncodeToString(digest[:]),
		now+int64(24*time.Hour/time.Millisecond),
		now,
		now,
	); err != nil {
		return Session{}, fmt.Errorf("create upload: %w", err)
	}
	files := make([]File, 0, len(request.Files))
	for _, declaration := range request.Files {
		fileID, idErr := uuid.NewV7()
		if idErr != nil {
			return Session{}, fmt.Errorf("generate upload file id: %w", idErr)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_files(id,
upload_session_id,
relative_path,
declared_size_bytes,
state,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
'PENDING',
?,
?)
`, fileID.String(), uploadID.String(), declaration.RelativePath, declaration.SizeBytes, now, now); err != nil {
			return Session{}, fmt.Errorf("create upload file: %w", err)
		}
		files = append(
			files,
			File{
				ID:           fileID.String(),
				ClientFileID: declaration.ClientFileID,
				RelativePath: declaration.RelativePath,
				SizeBytes:    declaration.SizeBytes,
				State:        "PENDING",
				Parts:        []int{},
			},
		)
	}
	if err := transaction.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit upload: %w", err)
	}
	return Session{
		ID:             uploadID.String(),
		State:          "CREATED",
		SourceType:     request.SourceType,
		TotalBytes:     total,
		Version:        1,
		ExpiresAtMS:    now + int64(24*time.Hour/time.Millisecond),
		ChunkSizeBytes: PartSize,
		Files:          files,
	}, nil
}

func (service *Service) Get(ctx context.Context, uploadID string) (Session, error) {
	var result Session
	var finalizeJob sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT id,
state,
source_type,
total_bytes,
finalization_no,
finalize_job_id,
version,
expires_at_ms
FROM upload_sessions
WHERE id=?
`, uploadID).
		Scan(
			&result.ID,
			&result.State,
			&result.SourceType,
			&result.TotalBytes,
			&result.FinalizationNo,
			&finalizeJob,
			&result.Version,
			&result.ExpiresAtMS,
		)
	if err != nil {
		return Session{}, fmt.Errorf("uploads/service: %w", err)
	}
	result.ChunkSizeBytes = PartSize
	if finalizeJob.Valid {
		result.FinalizeJobID = finalizeJob.String
	}
	result.Files, err = service.files(ctx, uploadID)
	if err != nil {
		return Session{}, err
	}
	for index := range result.Files {
		result.Files[index].Parts, err = service.partNumbers(ctx, result.Files[index].ID)
		if err != nil {
			return Session{}, err
		}
	}
	return result, nil
}

func (service *Service) files(ctx context.Context, uploadID string) ([]File, error) {
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT id,
relative_path,
declared_size_bytes,
received_size_bytes,
state
FROM upload_files
WHERE upload_session_id=?
ORDER BY relative_path,
id
`,
		uploadID,
	)
	if err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]File, 0)
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.RelativePath, &file.SizeBytes, &file.Received, &file.State); err != nil {
			return nil, fmt.Errorf("uploads/service: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan upload files: %w", err)
	}
	return files, nil
}

func (service *Service) partNumbers(ctx context.Context, fileID string) ([]int, error) {
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT part_no
FROM upload_parts
WHERE upload_file_id=?
ORDER BY part_no
`,
		fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	parts := make([]int, 0)
	for rows.Next() {
		var part int
		if err := rows.Scan(&part); err != nil {
			return nil, fmt.Errorf("uploads/service: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	return parts, nil
}

type byteRange struct{ start, end, total int64 }

func parseRange(value string) (byteRange, error) {
	if !strings.HasPrefix(value, "bytes ") {
		return byteRange{}, ErrInvalid
	}
	span, totalText, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	startText, endText, okSpan := strings.Cut(span, "-")
	start, startErr := strconv.ParseInt(startText, 10, 64)
	end, endErr := strconv.ParseInt(endText, 10, 64)
	total, totalErr := strconv.ParseInt(totalText, 10, 64)
	if !ok || !okSpan || startErr != nil || endErr != nil || totalErr != nil || start < 0 || end < start ||
		total <= end ||
		end-start+1 > PartSize {
		return byteRange{}, ErrInvalid
	}
	return byteRange{start: start, end: end, total: total}, nil
}

func parseDigest(value string) (string, error) {
	if !strings.HasPrefix(value, "sha-256=:") || !strings.HasSuffix(value, ":") {
		return "", ErrInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(value, "sha-256=:"), ":"))
	if err != nil || len(decoded) != 32 {
		return "", ErrInvalid
	}
	return hex.EncodeToString(decoded), nil
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) PutPart(
	ctx context.Context,
	uploadID, fileID string,
	partNo int,
	contentRange, contentDigest string,
	body io.Reader,
) error {
	parsedRange, err := parseRange(contentRange)
	if err != nil || partNo < 0 || parsedRange.start/PartSize != int64(partNo) {
		return ErrInvalid
	}
	expectedDigest, err := parseDigest(contentDigest)
	if err != nil {
		return err
	}
	var declaredSize int64
	var state string
	if err := service.database.QueryRowContext(ctx, `
SELECT declared_size_bytes,
state
FROM upload_files
WHERE id=?
AND upload_session_id=?
`, fileID, uploadID).Scan(&declaredSize, &state); err != nil {
		return fmt.Errorf("uploads/service: %w", err)
	}
	if declaredSize != parsedRange.total || state == "COMPLETE" || state == "FINALIZING" {
		return ErrInvalid
	}
	directory := filepath.Join(service.dataDir, "tmp", "uploads", uploadID, fileID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("uploads/service: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".part-")
	if err != nil {
		return fmt.Errorf("uploads/service: %w", err)
	}
	name := temporary.Name()
	defer cleanup.Remove(name)
	_ = temporary.Chmod(0o600)
	hash := sha256.New()
	written, copyErr := io.Copy(
		io.MultiWriter(temporary, hash),
		io.LimitReader(body, parsedRange.end-parsedRange.start+2),
	)
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil || written != parsedRange.end-parsedRange.start+1 ||
		hex.EncodeToString(hash.Sum(nil)) != expectedDigest {
		return ErrInvalid
	}
	target := filepath.Join(directory, strconv.Itoa(partNo))
	if err := os.Link(name, target); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("uploads/service: %w", err)
	}
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(
		ctx,
		`
INSERT INTO upload_parts(upload_file_id,
part_no,
offset_bytes,
size_bytes,
sha256,
storage_key,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?) ON CONFLICT(upload_file_id,
part_no) DO NOTHING
`,
		fileID,
		partNo,
		parsedRange.start,
		written,
		expectedDigest,
		filepath.ToSlash(filepath.Join(uploadID, fileID, strconv.Itoa(partNo))),
		now,
	)
	if err != nil {
		return fmt.Errorf("uploads/service: %w", err)
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		var existing string
		if err := service.database.QueryRowContext(ctx, `
SELECT sha256
FROM upload_parts
WHERE upload_file_id=?
AND part_no=?
`, fileID, partNo).Scan(&existing); err != nil ||
			existing != expectedDigest {
			return ErrInvalid
		}
		return nil
	}
	if _, err := service.database.ExecContext(ctx, `
UPDATE upload_files
SET received_size_bytes=received_size_bytes+?,
state='PARTIAL',
updated_at_ms=?
WHERE id=?
`, written, now, fileID); err != nil {
		return fmt.Errorf("uploads/service: %w", err)
	}
	_, err = service.database.ExecContext(
		ctx,
		`
UPDATE upload_sessions
SET state='UPLOADING',
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		now,
		uploadID,
	)
	if err != nil {
		return fmt.Errorf("mark upload session active: %w", err)
	}
	return nil
}

//nolint:funlen // Completeness checks, job creation, optimistic transition, and event emission share one transaction.
func (service *Service) Complete(ctx context.Context, uploadID string, version int64) (string, int64, error) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, fmt.Errorf("uploads/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentVersion, finalization int64
	var state string
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version,
finalization_no
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&state, &currentVersion, &finalization); err != nil {
		return "", 0, fmt.Errorf("uploads/service: %w", err)
	}
	if currentVersion != version || state != "UPLOADING" && state != "CREATED" && state != "FAILED" {
		return "", 0, ErrInvalid
	}
	jobID, _ := uuid.NewV7()
	finalization++
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", uploadID, finalization)))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'UPLOAD_SESSION',
?,
'UPLOAD_FINALIZE',
?,
1,
?,
1,
'QUEUED',
0,
2,
?,
?,
?)
`,
		jobID.String(),
		uploadID,
		hex.EncodeToString(dedupe[:]),
		fmt.Sprintf(`{"uploadId":%q,"finalizationNo":%d}`, uploadID, finalization),
		now,
		now,
		now,
	); err != nil {
		return "", 0, fmt.Errorf("uploads/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET state='FINALIZING',
finalization_no=?,
finalize_job_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?;
 UPDATE upload_files
SET state='FINALIZING',
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE'
`, finalization, jobID.String(), now, uploadID, now, uploadID); err != nil {
		return "", 0, fmt.Errorf("uploads/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", 0, fmt.Errorf("uploads/service: %w", err)
	}
	go service.finalize(context.WithoutCancel(ctx), uploadID, jobID.String())
	return jobID.String(), finalization, nil
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Cancel(ctx context.Context, uploadID string, version int64) (Canceled, bool, error) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var currentVersion int64
	var jobID sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version,
finalize_job_id
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&state, &currentVersion, &jobID); err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	if currentVersion != version || state == "COMPLETE" || state == "CANCELLED" || state == "EXPIRED" {
		return Canceled{}, false, ErrInvalid
	}
	pending := state == "FINALIZING"
	newVersion := currentVersion + 1
	if pending {
		var jobState string
		if err := transaction.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID.String).Scan(&jobState); err != nil {
			return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
		}
		result, err := transaction.ExecContext(
			ctx,
			`
UPDATE jobs
SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
cancel_requested_at_ms=?,
cancel_reason='upload cancelled',
finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE finished_at_ms END,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state IN ('QUEUED',
'RUNNING')
`,
			now,
			now,
			now,
			jobID.String,
		)
		if err != nil {
			return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
		}
		changed, _ := result.RowsAffected()
		pending = changed > 0 && jobState == "RUNNING"
	}
	if !pending {
		if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET state='CANCELLED',
version=?,
updated_at_ms=?
WHERE id=?;
 UPDATE upload_files
SET state='FAILED',
last_error_code='UPLOAD_CANCELLED',
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE'
`, newVersion, now, uploadID, now, uploadID); err != nil {
			return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
		}
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET version=?,
updated_at_ms=?
WHERE id=?
`, newVersion, now, uploadID); err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	if !pending {
		// The directory is derived from the already validated UUID route value.
		cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", uploadID))
	}
	resultState := "CANCELLED"
	if pending {
		resultState = "CANCEL_REQUESTED"
	}
	return Canceled{UploadID: uploadID, State: resultState, Version: newVersion}, pending, nil
}

type part struct {
	offset, size int64
	path         string
}

func (service *Service) fileParts(ctx context.Context, fileID string) ([]part, error) {
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT offset_bytes,
size_bytes,
storage_key
FROM upload_parts
WHERE upload_file_id=?
ORDER BY offset_bytes
`,
		fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	parts := make([]part, 0)
	for rows.Next() {
		var value part
		if err := rows.Scan(&value.offset, &value.size, &value.path); err != nil {
			return nil, fmt.Errorf("uploads/service: %w", err)
		}
		parts = append(parts, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	return parts, nil
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) finalize(parent context.Context, uploadID, jobID string) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	now := service.now().UnixMilli()
	started, _ := service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='RUNNING',
attempt_count=1,
execution_started_at_ms=?,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
		now,
		now,
		jobID,
	)
	if rows, _ := started.RowsAffected(); rows != 1 {
		return
	}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT id,
declared_size_bytes
FROM upload_files
WHERE upload_session_id=?
AND state!='COMPLETE'
ORDER BY id
`,
		uploadID,
	)
	if err != nil {
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type candidate struct {
		id   string
		size int64
	}
	var files []candidate
	for rows.Next() {
		var file candidate
		if err := rows.Scan(&file.id, &file.size); err != nil {
			service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
			return
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	for _, file := range files {
		if service.cancelRequested(ctx, uploadID, jobID) {
			return
		}
		parts, queryErr := service.fileParts(ctx, file.id)
		if queryErr != nil {
			service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
			return
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].offset < parts[j].offset })
		var offset int64
		var handles []*os.File
		var readers []io.Reader
		valid := true
		for _, value := range parts {
			if value.offset != offset {
				valid = false
				break
			}
			handle, openErr := os.Open(filepath.Join(service.dataDir, "tmp", "uploads", filepath.FromSlash(value.path)))
			if openErr != nil {
				valid = false
				break
			}
			handles = append(handles, handle)
			readers = append(readers, io.LimitReader(handle, value.size))
			offset += value.size
		}
		if offset != file.size {
			valid = false
		}
		if !valid {
			for _, handle := range handles {
				cleanup.Error("close", handle.Close())
			}
			service.fail(ctx, uploadID, jobID, "UPLOAD_PART_MISSING")
			return
		}
		metadata, putErr := service.blobs.Put(io.MultiReader(readers...))
		for _, handle := range handles {
			cleanup.Error("close", handle.Close())
		}
		if putErr != nil {
			service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
			return
		}
		blobID := ""
		lookupErr := service.database.QueryRowContext(ctx, `
SELECT id
FROM blobs
WHERE sha256=?
`, metadata.SHA256).
			Scan(&blobID)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			generated, _ := uuid.NewV7()
			blobID = generated.String()
			if _, insertErr := service.database.ExecContext(ctx, `
INSERT INTO blobs(id,
sha256,
size_bytes,
md5,
sha1,
crc32,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?)
`,
				blobID,
				metadata.SHA256,
				metadata.Size,
				metadata.MD5,
				metadata.SHA1,
				metadata.CRC32,
				"application/octet-stream",
				service.now().UnixMilli(),
			); insertErr != nil {
				service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
				return
			}
		} else if lookupErr != nil {
			service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
			return
		}
		if _, err := service.database.ExecContext(ctx, `
UPDATE upload_files
SET final_blob_id=?,
state='COMPLETE',
last_error_code=NULL,
updated_at_ms=?
WHERE id=?
`, blobID, service.now().UnixMilli(), file.id); err != nil {
			service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
			return
		}
	}
	if service.cancelRequested(ctx, uploadID, jobID) {
		return
	}
	now = service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET state='COMPLETE',
version=version+1,
expires_at_ms=?,
last_error_code=NULL,
updated_at_ms=?
WHERE id=?
`, now+int64(7*24*time.Hour/time.Millisecond), now, uploadID); err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
updated_at_ms=?
WHERE id=?
`, now, now, jobID); err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'UPLOAD_SESSION',
?,
'SUCCEEDED',
'{}',
?)
`, jobID, uploadID, now); err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	_ = transaction.Commit()
}

func (service *Service) cancelRequested(ctx context.Context, uploadID, jobID string) bool {
	var state string
	if err := service.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil ||
		state != "CANCEL_REQUESTED" {
		return false
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return true
	}
	defer cleanup.Rollback(transaction)
	_, err = transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='CANCELLED',
finished_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='CANCEL_REQUESTED';
 UPDATE upload_sessions
SET state='CANCELLED',
version=version+1,
updated_at_ms=?
WHERE id=?;
 UPDATE upload_files
SET state='FAILED',
last_error_code='UPLOAD_CANCELLED',
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE';
 INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'UPLOAD_SESSION',
?,
'CANCELLED',
'{}',
?)
`,
		now,
		now,
		jobID,
		now,
		uploadID,
		now,
		uploadID,
		jobID,
		uploadID,
		now,
	)
	if err != nil || transaction.Commit() != nil {
		return true
	}
	cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", uploadID))
	return true
}

func (service *Service) fail(ctx context.Context, uploadID, jobID, code string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE upload_sessions
SET state='FAILED',
version=version+1,
last_error_code=?,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		uploadID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE upload_files
SET state='FAILED',
last_error_code=?,
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE'
`,
		code,
		now,
		uploadID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=0,
finished_at_ms=?,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	)
}
