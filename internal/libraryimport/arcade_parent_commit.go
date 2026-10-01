package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

// Every accepted artifact and audit row must commit atomically. The legacy
// facade keeps the worker's domain orchestration here while the repository
// owns all SQL and transaction boundaries.
func (service *Service) commitAcceptedParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
	entries []importing.ArchiveEntry,
	files []attachedSourceFile,
	manifestJSON, manifestDigest string,
	validation preparedGroup,
	diagnostics map[string]any,
) error {
	copies, err := service.prepareParentDirectory(ctx, &candidate, files, &validation)
	if err != nil {
		return err
	}
	diagnosticsJSON, _ := json.Marshal(diagnostics)
	now := service.now().UnixMilli()
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	err = repository.CommitAccepted(ctx, libraryservice.ArcadeParentAcceptedCommit{
		Candidate: libraryservice.ArcadeParentCommitCandidate{
			AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, DraftID: candidate.draftID,
			BaseSnapshotID: candidate.baseSnapshotID, Machine: candidate.machine,
			ProviderID: candidate.providerID, TargetID: candidate.targetID, DATID: candidate.datID,
			UploadFileID: candidate.uploadFileID, UploadSessionID: candidate.uploadSessionID,
			FileRecord: candidate.fileRecord, BlobSHA: candidate.blobSHA, BlobSize: candidate.blobSize,
			ContentPolicyDigest: candidate.contentPolicyDigest,
		},
		JobID: jobID, WorkerID: workerID, Entries: entries,
		Files: arcadeParentSourceFiles(files), FileCopies: copies, ManifestJSON: manifestJSON, ManifestDigest: manifestDigest,
		Validation: libraryservice.ArcadeParentValidation{
			Status: validation.ValidationStatus, CompatibilityCode: validation.CompatibilityCode,
			DependencySnapshot: validation.DependencySnapshot,
			Files:              arcadeParentValidationFiles(validation.ValidationFiles),
		},
		DiagnosticsJSON: string(diagnosticsJSON),
		Actor:           reviewActor(ctx), NowMS: now,
	})
	if err != nil {
		return fmt.Errorf("commit accepted arcade parent attachment: %w", err)
	}
	return nil
}

func arcadeParentSourceFiles(files []attachedSourceFile) []libraryservice.ArcadeParentSourceFile {
	result := make([]libraryservice.ArcadeParentSourceFile, 0, len(files))
	for _, file := range files {
		var archiveFileRecord *string
		if file.archiveFileRecord.Valid {
			value := file.archiveFileRecord.String
			archiveFileRecord = &value
		}
		var archiveOrdinal *int
		if file.archiveOrdinal.Valid {
			value := int(file.archiveOrdinal.Int64)
			archiveOrdinal = &value
		}
		result = append(result, libraryservice.ArcadeParentSourceFile{
			Role: file.role, LogicalName: file.logicalName, UploadFileID: file.uploadFileID,
			FileRecord: file.fileRecord, BlobSHA: file.blobSHA, BlobSize: file.blobSize,
			ArchiveFileRecord: archiveFileRecord, ArchiveOrdinal: archiveOrdinal, SortOrder: file.sortOrder,
		})
	}
	return result
}

func arcadeParentValidationFiles(files []preparedValidationFile) []libraryservice.ArcadeParentValidationFile {
	result := make([]libraryservice.ArcadeParentValidationFile, 0, len(files))
	for _, file := range files {
		result = append(result, libraryservice.ArcadeParentValidationFile{
			Role: file.Role, LogicalName: file.LogicalName, FileRecord: file.FileRecord, SortOrder: file.SortOrder,
		})
	}
	return result
}

func (service *Service) finishRejectedParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code, archiveCode string,
	missing, mismatched []string,
) {
	if ctx.Err() != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return
	}
	ctx, cancel := attachmentCleanupContext(ctx)
	defer cancel()
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "archiveCode": archiveCode, "missingEntries": missing,
		"mismatchedEntries": mismatched,
	})
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	err := repository.FinishRejected(ctx, libraryservice.ArcadeParentRejectedCommit{
		AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, JobID: jobID, WorkerID: workerID,
		Code: code, DiagnosticsJSON: string(diagnostics),
		BlobSize: candidate.blobSize, BlobSHA: candidate.blobSHA, Actor: reviewActor(ctx),
		NowMS: service.now().UnixMilli(),
	})
	cleanup.Error("finish rejected parent attachment", err)
}

func (service *Service) finishRetryableParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code string,
) {
	ctx, cancel := attachmentCleanupContext(ctx)
	defer cancel()
	if service.finishParentAttachmentCancellation(ctx, candidate, jobID, workerID) {
		return
	}
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	err := repository.FinishRetryable(ctx, libraryservice.ArcadeParentRetryableCommit{
		AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, JobID: jobID, WorkerID: workerID,
		Code: code, DiagnosticsJSON: diagnostics, BlobSize: candidate.blobSize, BlobSHA: candidate.blobSHA,
		NowMS: service.now().UnixMilli(),
	})
	cleanup.Error("finish retryable parent attachment", err)
}

func (service *Service) SyncParentAttachmentCancellation(ctx context.Context, jobID string) {
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	err := repository.SyncCancellation(ctx, libraryservice.ArcadeParentCancellationSync{
		JobID: jobID, NowMS: service.now().UnixMilli(),
	})
	cleanup.Error("sync parent attachment cancellation", err)
}

func (service *Service) finishParentAttachmentCancellation(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) bool {
	ctx, cancel := attachmentCleanupContext(ctx)
	defer cancel()
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	ok, err := repository.FinishCancellation(ctx, libraryservice.ArcadeParentAttachmentCancellation{
		AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, JobID: jobID, WorkerID: workerID,
		NowMS: service.now().UnixMilli(),
	})
	cleanup.Error("finish parent attachment cancellation", err)
	return ok
}
