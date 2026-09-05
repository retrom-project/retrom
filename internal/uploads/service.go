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
	Purpose    string            `json:"purpose,omitempty"`
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
	Purpose        string `json:"purpose"`
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

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Session, error) {
	if request.Purpose == "" {
		request.Purpose = "GENERAL"
	}
	total, err := validateCreateRequest(request)
	if err != nil {
		return Session{}, err
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
purpose,
state,
source_type,
total_files,
total_bytes,
manifest_digest,
expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
?,
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
		request.Purpose,
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
		Purpose:        request.Purpose,
		SourceType:     request.SourceType,
		TotalBytes:     total,
		Version:        1,
		ExpiresAtMS:    now + int64(24*time.Hour/time.Millisecond),
		ChunkSizeBytes: PartSize,
		Files:          files,
	}, nil
}

func validateCreateRequest(request CreateRequest) (int64, error) {
	if !validUploadShape(request) {
		return 0, ErrInvalid
	}
	if request.Purpose != "GENERAL" && request.SourceType == "FILES" &&
		(len(request.Files) != 1 || !isProjectUpload(request.Purpose, request.Files[0].RelativePath)) {
		return 0, ErrInvalid
	}
	seen := make(map[string]struct{}, len(request.Files))
	var total int64
	for _, file := range request.Files {
		if !validUploadFile(file) {
			return 0, ErrInvalid
		}
		if _, duplicate := seen[file.RelativePath]; duplicate {
			return 0, ErrInvalid
		}
		seen[file.RelativePath] = struct{}{}
		total += file.SizeBytes
		if total > 32<<30 {
			return 0, ErrInvalid
		}
	}
	return total, nil
}

func validUploadShape(request CreateRequest) bool {
	validPurpose := request.Purpose == "GENERAL" || request.Purpose == "PROJECT" ||
		request.Purpose == "RUNTIME_ASSET_PACK"
	validSource := request.SourceType == "FILES" || request.SourceType == "DIRECTORY"
	return validPurpose && validSource && len(request.Files) >= 1 && len(request.Files) <= 10_000
}

func validUploadFile(file FileDeclaration) bool {
	if file.ClientFileID == "" || file.SizeBytes < 0 || file.SizeBytes > 8<<30 ||
		len([]byte(file.RelativePath)) > 1024 {
		return false
	}
	_, err := importing.ValidateLogicalPath(file.RelativePath)
	return err == nil
}

func isProjectUpload(purpose, relativePath string) bool {
	extension := strings.ToLower(filepath.Ext(relativePath))
	return extension == ".zip" || extension == ".7z" ||
		purpose == "PROJECT" && extension == ".exe"
}

func (service *Service) Get(ctx context.Context, uploadID string) (Session, error) {
	var result Session
	var finalizeJob sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT id,
state,
purpose,
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
			&result.Purpose,
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

// Contract branches stay contiguous for a single auditable decision.
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
	if err := service.validatePartTarget(ctx, uploadID, fileID, parsedRange.total); err != nil {
		return err
	}
	written, err := service.stageUploadPart(uploadID, fileID, partNo, parsedRange, expectedDigest, body)
	if err != nil {
		return err
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

func (service *Service) validatePartTarget(
	ctx context.Context,
	uploadID, fileID string,
	total int64,
) error {
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
	if declaredSize != total || state == "COMPLETE" || state == "FINALIZING" {
		return ErrInvalid
	}
	return nil
}

func (service *Service) stageUploadPart(
	uploadID, fileID string,
	partNo int,
	parsedRange byteRange,
	expectedDigest string,
	body io.Reader,
) (int64, error) {
	directory := filepath.Join(service.dataDir, "tmp", "uploads", uploadID, fileID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return 0, fmt.Errorf("uploads/service: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".part-")
	if err != nil {
		return 0, fmt.Errorf("uploads/service: %w", err)
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
		return 0, ErrInvalid
	}
	target := filepath.Join(directory, strconv.Itoa(partNo))
	if err := os.Link(name, target); err != nil && !errors.Is(err, os.ErrExist) {
		return 0, fmt.Errorf("uploads/service: %w", err)
	}
	return written, nil
}

// Completeness checks, job creation, optimistic transition, and event emission share one transaction.
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
