package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/metadatascrape"
	"retrom/internal/tagging"
)

type Service struct {
	database               *sql.DB
	blobs                  *blobstore.Store
	now                    func() time.Time
	scraper                *metadatascrape.Service
	tags                   *tagging.Service
	multiDiscImportEnabled bool
	importGroupSlots       chan struct{}
	importGroupMu          sync.Mutex
	importGroupCancels     map[string]context.CancelFunc
}

func (service *Service) WithMultiDiscImportEnabled(enabled bool) *Service {
	service.multiDiscImportEnabled = enabled
	return service
}

func reviewActor(ctx context.Context) authn.Actor {
	return authn.ActorFromContext(ctx, "release-setup")
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func New(database *sql.DB, now func() time.Time, scraper ...*metadatascrape.Service) *Service {
	service := &Service{
		database: database, now: now, tags: tagging.New(database, now),
		importGroupSlots: make(chan struct{}, 1), importGroupCancels: make(map[string]context.CancelFunc),
	}
	if len(scraper) > 0 {
		service.scraper = scraper[0]
	}
	return service
}

// Reconfigure reuses the unresolved rejected files from an existing import. The
// cloned UploadSession owns new logical UploadFiles but points at the same CAS
// blobs, so the browser never has to upload the bytes again.
func (service *Service) Reconfigure(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
	request ReconfigureRequest,
) (Created, error) {
	if sourceImportJobID == "" || expectedVersion < 1 {
		return Created{}, ErrInvalid
	}
	sourceType, files, err := service.reconfigurationSource(ctx, sourceImportJobID, expectedVersion)
	if err != nil {
		return Created{}, err
	}
	if len(files) == 0 {
		return Created{}, ErrInvalid
	}
	uploadID, err := service.cloneUploadSession(ctx, sourceImportJobID, expectedVersion, sourceType, files)
	if err != nil {
		return Created{}, err
	}
	sourceFileIDs := make([]string, 0, len(files))
	for _, file := range files {
		sourceFileIDs = append(sourceFileIDs, file.id)
	}
	created, err := service.create(
		ctx,
		CreateRequest{
			UploadID:                 uploadID,
			TargetPlatformInstanceID: request.TargetPlatformInstanceID,
			MetadataProvider:         request.MetadataProvider,
			TagIDs:                   request.TagIDs,
		},
		&reconfigurationInput{
			sourceImportJobID: sourceImportJobID,
			sourceVersion:     expectedVersion,
			sourceFileIDs:     sourceFileIDs,
		},
	)
	if err != nil {
		service.removeUnusedClonedUpload(ctx, uploadID)
		return Created{}, err
	}
	return created, nil
}

func (service *Service) reconfigurationSource(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
) (string, []reusableUploadFile, error) {
	var sourceType, state string
	var currentVersion int64
	if err := service.database.QueryRowContext(ctx, `
SELECT u.source_type,
i.state,
i.version
FROM import_jobs i
JOIN upload_sessions u ON u.id=i.upload_session_id
WHERE i.id=?
`, sourceImportJobID).Scan(&sourceType, &state, &currentVersion); err != nil ||
		state != "PARTIAL_FAILURE" || currentVersion != expectedVersion {
		return "", nil, ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT u.id,
u.relative_path,
u.declared_size_bytes,
u.final_blob_id
FROM import_job_files f
JOIN upload_files u ON u.id=f.upload_file_id
LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id
AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=?
AND f.disposition='REJECTED'
AND resolution.upload_file_id IS NULL
ORDER BY u.relative_path,
u.id
`, sourceImportJobID)
	if err != nil {
		return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]reusableUploadFile, 0)
	for rows.Next() {
		var file reusableUploadFile
		if err := rows.Scan(&file.id, &file.path, &file.size, &file.blobID); err != nil {
			return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	if err := rows.Close(); err != nil {
		return "", nil, fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	return sourceType, files, nil
}

func (service *Service) cloneUploadSession(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
	sourceType string,
	files []reusableUploadFile,
) (string, error) {
	uploadID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	digest := reconfigurationManifestDigest(sourceImportJobID, expectedVersion, files)
	now := service.now().UnixMilli()
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.size
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentState string
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version
FROM import_jobs
WHERE id=?
`, sourceImportJobID).Scan(&currentState, &currentVersion); err != nil ||
		currentState != "PARTIAL_FAILURE" || currentVersion != expectedVersion {
		return "", ErrInvalid
	}
	if err := insertClonedUpload(
		ctx, transaction, uploadID.String(), sourceType, files, digest, now, totalBytes,
	); err != nil {
		return "", err
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	return uploadID.String(), nil
}

func reconfigurationManifestDigest(sourceImportJobID string, sourceVersion int64, files []reusableUploadFile) string {
	manifestFiles := make([]map[string]any, 0, len(files))
	for _, file := range files {
		manifestFiles = append(manifestFiles, map[string]any{
			"sourceUploadFileId": file.id,
			"relativePath":       file.path,
			"sizeBytes":          file.size,
			"blobId":             file.blobID,
		})
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion":     1,
		"sourceImportJobId": sourceImportJobID,
		"sourceVersion":     sourceVersion,
		"files":             manifestFiles,
	})
	digest := sha256.Sum256(manifest)
	return hex.EncodeToString(digest[:])
}

func insertClonedUpload(
	ctx context.Context,
	transaction *sql.Tx,
	uploadID, sourceType string,
	files []reusableUploadFile,
	manifestDigest string,
	now, totalBytes int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_sessions(id,
state,
source_type,
total_files,
total_bytes,
manifest_digest,
version,
expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'COMPLETE',
?,
?,
?,
?,
1,
?,
?,
?)
`, uploadID, sourceType, len(files), totalBytes, manifestDigest,
		now+int64(24*time.Hour/time.Millisecond), now, now); err != nil {
		return fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	for _, file := range files {
		fileID, idErr := uuid.NewV7()
		if idErr != nil {
			return fmt.Errorf("libraryimport/reconfigure: %w", idErr)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_files(id,
upload_session_id,
relative_path,
declared_size_bytes,
received_size_bytes,
final_blob_id,
state,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
'COMPLETE',
?,
?)
`, fileID.String(), uploadID, file.path, file.size, file.size, file.blobID, now, now); err != nil {
			return fmt.Errorf("libraryimport/reconfigure: %w", err)
		}
	}
	return nil
}

func (service *Service) removeUnusedClonedUpload(ctx context.Context, uploadID string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	var consumptionCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM upload_consumptions
WHERE upload_session_id=?
`, uploadID).Scan(&consumptionCount); err != nil || consumptionCount != 0 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM upload_files WHERE upload_session_id=?`, uploadID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id=?`, uploadID); err != nil {
		return
	}
	_ = transaction.Commit()
}
