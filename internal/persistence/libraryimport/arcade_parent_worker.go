package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/jobinput"

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

func claimArcadeParentAttachmentRecords(ctx context.Context, tx dbapi.Tx, jobID, workerID string, now int64) error {
	return claimAttachmentRecords(
		ctx, tx, jobID, workerID, now, "REVIEW_ARCADE_PARENT_VALIDATE",
		libraryservice.ArcadeParentAttachmentDeadline.Milliseconds(),
	)
}

func readClaimedArcadeParentAttachment(
	ctx context.Context, tx dbapi.Tx, jobID, workerID string,
) (libraryservice.ArcadeParentAttachmentWorkerClaim, error) {
	var result libraryservice.ArcadeParentAttachmentWorkerClaim
	result.JobID, result.WorkerID = jobID, workerID
	var inputJSON, scopeID string
	if err := dbapi.QueryRowContext(ctx, tx, `
SELECT input.input_json,job.scope_id,job.execution_started_at_ms,job.execution_deadline_at_ms
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
`, jobID).Scan(&inputJSON, &scopeID, &result.ExecutionStartedAtMS, &result.DeadlineAtMS); err != nil {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, fmt.Errorf("read arcade parent input: %w", err)
	}
	envelope, err := jobinput.Decode(
		[]byte(inputJSON), "REVIEW_ARCADE_PARENT_VALIDATE", jobinput.Scope{Type: "IMPORT_ITEM", ID: scopeID},
	)
	if err != nil {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, fmt.Errorf("decode attachment input: %w", err)
	}
	if err := json.Unmarshal(envelope.Inputs, &result.Input); err != nil ||
		!libraryservice.ValidArcadeParentAttachmentInput(result.Input) {
		return libraryservice.ArcadeParentAttachmentWorkerClaim{}, libraryservice.ErrInvalid
	}
	var candidate libraryservice.ArcadeParentAttachmentCandidate
	if err := dbapi.QueryRowContext(ctx, tx, `
SELECT attachment.id,attachment.import_item_id,attachment.review_draft_id,
attachment.base_source_snapshot_id,attachment.dependency_machine,attachment.required_by_machine,
attachment.depth,attachment.provider_id,attachment.target_id,
attachment.dat_version_id,attachment.upload_file_id,attachment.original_filename
FROM review_arcade_parent_attachments attachment
WHERE attachment.job_id=? AND attachment.state='PENDING'
`, jobID).Scan(
		&candidate.AttachmentID, &candidate.ItemID, &candidate.DraftID, &candidate.BaseSnapshotID,
		&candidate.Machine, &candidate.RequiredBy, &candidate.Depth, &candidate.ProviderID, &candidate.TargetID,
		&candidate.DATID, &candidate.UploadFileID, &candidate.OriginalName,
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
	candidate.UploadSessionID, candidate.FileRecord = result.Input.UploadSessionID, result.Input.FileRecord
	candidate.BlobSHA, candidate.BlobSize = result.Input.SHA256, result.Input.SizeBytes
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
SELECT file.role,file.logical_name,file.upload_file_id,file.file_record,((blob.value)::jsonb #>> '{sha256}'),
 (((blob.value)::jsonb #>> '{size_bytes}'))::bigint,
file.source_archive_file_record,file.source_archive_entry_ordinal,COALESCE(((archive.value)::jsonb #>>
 '{sha256}'),'')
FROM import_item_source_snapshot_files file
JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
LEFT JOIN LATERAL (SELECT file.source_archive_file_record AS value) archive ON archive.value IS NOT NULL
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
