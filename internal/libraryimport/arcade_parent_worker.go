package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/contentmanifest"
	"retrom/internal/importing"
)

// Claim, scan, exact DAT match, revalidation, and atomic commit form one worker contract.
func (service *Service) runParentAttachment(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, parentAttachmentDeadline)
	defer cancel()
	candidate, workerID, err := service.claimParentAttachment(ctx, jobID)
	if err != nil {
		return
	}
	archive, ok := service.validateParentArchive(ctx, candidate, jobID, workerID)
	if !ok {
		return
	}
	commit, ok := service.prepareParentCommit(ctx, candidate, jobID, workerID)
	if !ok {
		return
	}
	if err := service.commitAcceptedParentAttachment(
		ctx, candidate, jobID, workerID, archive.entries, commit.files,
		commit.manifestJSON, commit.manifestDigest, commit.group, archive.diagnostics,
	); err != nil {
		if service.finishParentAttachmentCancellation(ctx, candidate, jobID, workerID) {
			return
		}
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorInputStale)
	}
}

type validatedParentArchive struct {
	entries     []importing.ArchiveEntry
	diagnostics map[string]any
}

func (service *Service) validateParentArchive(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) (validatedParentArchive, bool) {
	entries, err := importing.ScanZIP(
		ctx, service.blobs.Path(candidate.blobSHA), importing.DefaultArchiveLimits(),
	)
	if err != nil {
		code := ParentErrorArchiveUnsafe
		if errors.Is(err, importing.ErrNestedArchiveUnsupported) {
			code = ParentErrorStructure
		}
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID, code, archiveReason(err), nil, nil,
		)
		return validatedParentArchive{}, false
	}
	entryByName, ignoredNestedEntries := rootParentEntries(entries)
	if len(entryByName) == 0 {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID,
			ParentErrorStructure, "ROOT_ROM_ENTRIES_MISSING", nil, nil,
		)
		return validatedParentArchive{}, false
	}
	requirements, hasDisk, err := service.arcadeRequirements(ctx, candidate.datID, candidate.machine)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return validatedParentArchive{}, false
	}
	if hasDisk {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID, ParentErrorStructure, "UNSUPPORTED_CHD", nil, nil,
		)
		return validatedParentArchive{}, false
	}
	missing, mismatched, warnings := matchArcadeRequirements(entryByName, requirements)
	if len(missing) != 0 || len(mismatched) != 0 {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID,
			ParentErrorMismatch, "DAT_ENTRY_MISMATCH", missing, mismatched,
		)
		return validatedParentArchive{}, false
	}
	return validatedParentArchive{
		entries: entries,
		diagnostics: map[string]any{
			"schemaVersion": 1, "requiredEntryCount": len(requirements),
			"observedEntryCount": len(entries), "observedRootEntryCount": len(entryByName),
			"ignoredNestedEntryCount": ignoredNestedEntries, "warnings": warnings,
		},
	}, true
}

type preparedParentCommit struct {
	files          []attachedSourceFile
	manifestJSON   string
	manifestDigest string
	group          preparedGroup
}

func (service *Service) prepareParentCommit(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) (preparedParentCommit, bool) {
	files, manifestJSON, manifestDigest, err := service.buildAttachedSourceSnapshot(ctx, candidate)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	preparedFiles := make([]importSourceFile, 0, len(files))
	for _, file := range files {
		preparedFiles = append(preparedFiles, importSourceFile{
			id: file.uploadFileID, path: file.logicalName, blobID: file.blobID, sha256: file.blobSHA,
		})
	}
	_, groups, _ := service.prepareArcadeFiles(
		ctx, preparedFiles, sql.NullString{String: candidate.datID, Valid: true},
	)
	rootMachine, err := service.parentAttachmentRootMachine(ctx, candidate)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	group := selectParentGroup(groups, rootMachine)
	if group == nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return preparedParentCommit{}, false
	}
	return preparedParentCommit{
		files: files, manifestJSON: manifestJSON, manifestDigest: manifestDigest, group: *group,
	}, true
}

func selectParentGroup(groups []preparedGroup, rootMachine string) *preparedGroup {
	for index := range groups {
		for _, source := range groups[index].sources {
			if source.role == "CONTENT" && source.logicalName == rootMachine+".zip" {
				return &groups[index]
			}
		}
	}
	return nil
}

func rootParentEntries(entries []importing.ArchiveEntry) (map[string]importing.ArchiveEntry, int) {
	rootEntries := make(map[string]importing.ArchiveEntry, len(entries))
	ignoredNestedEntries := 0
	for _, entry := range entries {
		if strings.Contains(entry.NormalizedPath, "/") {
			ignoredNestedEntries++
			continue
		}
		rootEntries[entry.NormalizedPath] = entry
	}
	return rootEntries, ignoredNestedEntries
}

func (service *Service) parentAttachmentRootMachine(
	ctx context.Context,
	candidate parentAttachmentCandidate,
) (string, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=? AND source_snapshot_id=? AND provider_id=? AND target_id=? AND dat_version_id=?
ORDER BY created_at_ms DESC,id DESC LIMIT 1
	`, candidate.itemID, candidate.baseSnapshotID, candidate.providerID,
		candidate.targetID, candidate.datID).Scan(&raw); err != nil {
		return "", parentStoreError("read root validation", err)
	}
	snapshot, valid := parseArcadeDraftSnapshot(raw)
	if !valid {
		return "", ErrInvalid
	}
	return snapshot.Machine, nil
}

func (service *Service) claimParentAttachment(
	ctx context.Context,
	jobID string,
) (parentAttachmentCandidate, string, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("begin claim", err)
	}
	defer cleanup.Rollback(transaction)
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),execution_deadline_at_ms=?,
leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_ARCADE_PARENT_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
`, workerID.String(), now, now+int64(parentAttachmentDeadline/time.Millisecond),
		now+int64(parentAttachmentDeadline/time.Millisecond), now, now, jobID, now)
	if err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("claim job", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return parentAttachmentCandidate{}, "", ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments
SET state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')
	`, now, jobID); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("mark attachment running", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'STARTED','{}',? FROM jobs WHERE id=?
	`, now, jobID); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("record attachment start", err)
	}
	var candidate parentAttachmentCandidate
	if err := transaction.QueryRowContext(ctx, `
SELECT attachment.id,attachment.import_item_id,attachment.review_draft_id,
attachment.base_source_snapshot_id,attachment.dependency_machine,attachment.required_by_machine,
attachment.depth,attachment.provider_id,attachment.target_id,
attachment.dat_version_id,attachment.upload_file_id,
file.upload_session_id,attachment.original_filename,file.final_blob_id,blob.sha256,blob.size_bytes
FROM review_arcade_parent_attachments attachment
JOIN upload_files file ON file.id=attachment.upload_file_id
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE attachment.job_id=? AND attachment.state='RUNNING'
`, jobID).Scan(
		&candidate.attachmentID, &candidate.itemID, &candidate.draftID, &candidate.baseSnapshotID,
		&candidate.machine, &candidate.requiredBy, &candidate.depth, &candidate.providerID, &candidate.targetID,
		&candidate.datID,
		&candidate.uploadFileID, &candidate.uploadSessionID, &candidate.originalName, &candidate.blobID,
		&candidate.blobSHA, &candidate.blobSize,
	); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("read claimed attachment", err)
	}
	var frozenInputJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT input.input_json
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
`, jobID).Scan(&frozenInputJSON); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("read attachment input", err)
	}
	var frozenInput parentAttachmentInput
	if err := json.Unmarshal([]byte(frozenInputJSON), &frozenInput); err != nil ||
		frozenInput.ProviderID != candidate.providerID || frozenInput.TargetID != candidate.targetID ||
		len(frozenInput.ContentPolicyDigest) != 64 {
		return parentAttachmentCandidate{}, "", ErrInvalid
	}
	candidate.contentPolicyDigest = frozenInput.ContentPolicyDigest
	if err := transaction.Commit(); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("commit claim", err)
	}
	return candidate, workerID.String(), nil
}

type attachedSourceFile struct {
	role, logicalName, uploadFileID, blobID, blobSHA string
	blobSize                                         int64
	archiveBlobID                                    sql.NullString
	archiveOrdinal                                   sql.NullInt64
	sortOrder                                        int
}

func (service *Service) buildAttachedSourceSnapshot(
	ctx context.Context,
	candidate parentAttachmentCandidate,
) ([]attachedSourceFile, string, string, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.blob_id,blob.sha256,blob.size_bytes,
file.source_archive_blob_id,file.source_archive_entry_ordinal
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.source_snapshot_id=?
ORDER BY file.role,file.logical_name
	`, candidate.baseSnapshotID)
	if err != nil {
		return nil, "", "", parentStoreError("read source snapshot", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]attachedSourceFile, 0)
	logicalName := candidate.machine + ".zip"
	replaced := false
	for rows.Next() {
		var file attachedSourceFile
		if err := rows.Scan(
			&file.role, &file.logicalName, &file.uploadFileID, &file.blobID, &file.blobSHA, &file.blobSize,
			&file.archiveBlobID, &file.archiveOrdinal,
		); err != nil {
			return nil, "", "", parentStoreError("scan source snapshot", err)
		}
		if importing.ASCIICaseFold(file.logicalName) == importing.ASCIICaseFold(logicalName) {
			if file.role != "COMPANION" {
				return nil, "", "", ErrInvalid
			}
			file = attachedSourceFile{
				role: "COMPANION", logicalName: logicalName, uploadFileID: candidate.uploadFileID,
				blobID: candidate.blobID, blobSHA: candidate.blobSHA, blobSize: candidate.blobSize,
			}
			replaced = true
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, "", "", parentStoreError("iterate source snapshot", err)
	}
	if !replaced {
		files = append(files, attachedSourceFile{
			role: "COMPANION", logicalName: logicalName, uploadFileID: candidate.uploadFileID,
			blobID: candidate.blobID, blobSHA: candidate.blobSHA, blobSize: candidate.blobSize,
		})
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].role != files[right].role {
			return files[left].role < files[right].role
		}
		return files[left].logicalName < files[right].logicalName
	})
	manifestFiles := make([]contentmanifest.File, 0, len(files))
	for index := range files {
		file := &files[index]
		file.sortOrder = index
		manifest := contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.blobSHA, SizeBytes: file.blobSize,
		}
		if file.archiveBlobID.Valid {
			var archiveSHA string
			if err := service.database.QueryRowContext(ctx, `SELECT sha256 FROM blobs WHERE id=?`, file.archiveBlobID.String).
				Scan(&archiveSHA); err != nil {
				return nil, "", "", parentStoreError("read source archive", err)
			}
			ordinal := int(file.archiveOrdinal.Int64)
			manifest.SourceArchiveSHA256 = &archiveSHA
			manifest.SourceArchiveEntryOrdinal = &ordinal
		}
		manifestFiles = append(manifestFiles, manifest)
	}
	contents, digest, err := contentmanifest.Build("SINGLE_FILE", manifestFiles)
	return files, string(contents), digest, err
}
