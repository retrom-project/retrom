package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/cleanup"
	libraryservice "retrom/internal/service/libraryimport"
)

var errArcadeParentSourceArchiveBlobMissing = errors.New("arcade parent source archive blob is missing")

// ArcadeParentAttachmentWorker owns the short claim transaction and the
// worker read models used while validating an uploaded arcade parent. Archive
// parsing and source-manifest construction remain in the application facade.
type ArcadeParentAttachmentWorker struct{ database dbapi.DB }

var _ libraryservice.ArcadeParentAttachmentWorkerRepository = (*ArcadeParentAttachmentWorker)(nil)

func NewArcadeParentAttachmentWorker(database dbapi.DB) *ArcadeParentAttachmentWorker {
	return &ArcadeParentAttachmentWorker{database: database}
}

func (repository *ArcadeParentAttachmentWorker) Claim(
	ctx context.Context, jobID, workerID string, now int64,
) (libraryservice.ArcadeParentAttachmentWorkerClaim, error) {
	return runWorkerClaim(
		ctx, repository.database, jobID, workerID, now, "arcade parent",
		claimArcadeParentAttachmentRecords, readClaimedArcadeParentAttachment,
	)
}

func claimArcadeParentAttachmentRecords(
	ctx context.Context, tx dbapi.Tx, jobID, workerID string, now int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),execution_deadline_at_ms=?,
leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_ARCADE_PARENT_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
`, workerID, now, now+libraryservice.ArcadeParentAttachmentDeadline.Milliseconds(),
		now+libraryservice.ArcadeParentAttachmentDeadline.Milliseconds(), now, now, jobID, now)
	if err != nil {
		return fmt.Errorf("claim arcade parent job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return libraryservice.ErrInvalid
	}
	result, err = tx.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments
SET state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')
`, now, jobID)
	if err != nil {
		return fmt.Errorf("mark arcade parent attachment running: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return libraryservice.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'STARTED','{}',? FROM jobs WHERE id=?
`, now, jobID); err != nil {
		return fmt.Errorf("record arcade parent start event: %w", err)
	}
	return nil
}

func readClaimedArcadeParentAttachment(
	ctx context.Context, tx dbapi.Tx, jobID, workerID string,
) (libraryservice.ArcadeParentAttachmentWorkerClaim, error) {
	var result libraryservice.ArcadeParentAttachmentWorkerClaim
	result.JobID, result.WorkerID = jobID, workerID
	var inputJSON string
	if err := dbapi.QueryRowContext(ctx, tx, `
SELECT input.input_json,job.execution_started_at_ms
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
`, jobID).Scan(&inputJSON, &result.ExecutionStartedAtMS); err != nil {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, fmt.Errorf("read arcade parent input: %w", err)
	}
	if err := json.Unmarshal([]byte(inputJSON), &result.Input); err != nil ||
		!libraryservice.ValidArcadeParentAttachmentInput(result.Input) {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, libraryservice.ErrInvalid
	}
	var candidate libraryservice.ArcadeParentAttachmentCandidate
	if err := dbapi.QueryRowContext(ctx, tx, `
SELECT attachment.id,attachment.import_item_id,attachment.review_draft_id,
attachment.base_source_snapshot_id,attachment.dependency_machine,attachment.required_by_machine,
attachment.depth,attachment.provider_id,attachment.target_id,
attachment.dat_version_id,attachment.upload_file_id,
file.upload_session_id,attachment.original_filename,file.file_record,json_extract(blob.value,
'$.sha256'),json_extract(blob.value, '$.size_bytes')
FROM review_arcade_parent_attachments attachment
JOIN import_files file ON file.id=attachment.upload_file_id
JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
WHERE attachment.job_id=? AND attachment.state='RUNNING'
`, jobID).Scan(
		&candidate.AttachmentID, &candidate.ItemID, &candidate.DraftID, &candidate.BaseSnapshotID,
		&candidate.Machine, &candidate.RequiredBy, &candidate.Depth, &candidate.ProviderID, &candidate.TargetID,
		&candidate.DATID, &candidate.UploadFileID, &candidate.UploadSessionID, &candidate.OriginalName,
		&candidate.FileRecord, &candidate.BlobSHA, &candidate.BlobSize,
	); err != nil {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, fmt.Errorf(
			"read claimed arcade parent attachment: %w",
			err,
		)
	}
	if candidate.AttachmentID != result.Input.AttachmentID || candidate.ItemID != result.Input.ImportItemID ||
		candidate.DraftID != result.Input.ReviewDraftID || candidate.BaseSnapshotID != result.Input.BaseSourceSnapshotID ||
		candidate.Machine != result.Input.DependencyMachine || candidate.ProviderID != result.Input.ProviderID ||
		candidate.TargetID != result.Input.TargetID || candidate.DATID != result.Input.DATVersionID ||
		candidate.UploadFileID != result.Input.UploadFileID {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, libraryservice.ErrInvalid
	}
	candidate.ContentPolicyDigest = result.Input.ContentPolicyDigest
	result.Candidate = candidate
	return result, nil
}

func (repository *ArcadeParentAttachmentWorker) RootValidation(
	ctx context.Context, candidate libraryservice.ArcadeParentAttachmentCandidate,
) (string, error) {
	runtime, err := ReadReviewRuntime(ctx, repository.database, candidate.ItemID)
	if err != nil {
		return "", err
	}
	if runtime.SnapshotID != candidate.BaseSnapshotID ||
		runtime.ProviderID != candidate.ProviderID ||
		runtime.TargetID != candidate.TargetID ||
		runtime.DATID == nil ||
		*runtime.DATID != candidate.DATID {
		return "", libraryservice.ErrInvalid
	}
	return runtime.DependencyJSON, nil
}

func (repository *ArcadeParentAttachmentWorker) SourceSnapshot(
	ctx context.Context, snapshotID string,
) ([]libraryservice.ArcadeParentSourceSnapshotFile, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.file_record,json_extract(blob.value,
'$.sha256'),json_extract(blob.value, '$.size_bytes'),
file.source_archive_file_record,file.source_archive_entry_ordinal,COALESCE(json_extract(archive.value,
'$.sha256'),'')
FROM import_item_source_snapshot_files file
JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
LEFT JOIN json_each(json_array(file.source_archive_file_record)) archive ON archive.value IS NOT NULL
WHERE file.source_snapshot_id=?
ORDER BY file.role,file.logical_name
`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read arcade parent source snapshot: %w", err)
	}
	defer func() { cleanup.Error("close arcade parent source snapshot", rows.Close()) }()
	files := make([]libraryservice.ArcadeParentSourceSnapshotFile, 0)
	for rows.Next() {
		var file libraryservice.ArcadeParentSourceSnapshotFile
		var archiveID sql.NullString
		var archiveOrdinal sql.NullInt64
		if err := rows.Scan(
			&file.Role, &file.LogicalName, &file.UploadFileID, &file.FileRecord, &file.BlobSHA, &file.BlobSize,
			&archiveID, &archiveOrdinal, &file.SourceArchiveSHA,
		); err != nil {
			return nil, fmt.Errorf("scan arcade parent source snapshot: %w", err)
		}
		if archiveID.Valid {
			file.SourceArchiveFileRecord = archiveID.String
			if archiveOrdinal.Valid {
				ordinal := int(archiveOrdinal.Int64)
				file.SourceArchiveEntryOrdinal = &ordinal
			}
			if file.SourceArchiveSHA == "" {
				return nil, errArcadeParentSourceArchiveBlobMissing
			}
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate arcade parent source snapshot: %w", err)
	}
	return files, nil
}
