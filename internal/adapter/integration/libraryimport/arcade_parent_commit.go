package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/format/importing"
	libraryimportmodel "retrom/internal/model/libraryimport"
	librarypersistence "retrom/internal/repo/libraryimport"
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
	diagnosticsJSON, _ := json.Marshal(diagnostics)
	evidence := marshalReviewEventV2(map[string]any{
		"attachmentKind": "ARCADE_PARENT", "machine": candidate.machine,
		"validationStatus": validation.ValidationStatus, "state": "ACCEPTED",
	})
	now := service.now().UnixMilli()
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	err := repository.CommitAccepted(ctx, libraryimportmodel.ArcadeParentAcceptedCommit{
		Candidate: libraryimportmodel.ArcadeParentCommitCandidate{
			AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, DraftID: candidate.draftID,
			BaseSnapshotID: candidate.baseSnapshotID, Machine: candidate.machine,
			ProviderID: candidate.providerID, TargetID: candidate.targetID, DATID: candidate.datID,
			UploadFileID: candidate.uploadFileID, UploadSessionID: candidate.uploadSessionID,
			BlobID: candidate.blobID, BlobSHA: candidate.blobSHA, BlobSize: candidate.blobSize,
			ContentPolicyDigest: candidate.contentPolicyDigest,
		},
		JobID: jobID, WorkerID: workerID, Entries: entries,
		Files: arcadeParentSourceFiles(files), ManifestJSON: manifestJSON, ManifestDigest: manifestDigest,
		Validation: libraryimportmodel.ArcadeParentValidation{
			Status: validation.ValidationStatus, CompatibilityCode: validation.CompatibilityCode,
			DependencySnapshot: validation.DependencySnapshot,
			Files:              arcadeParentValidationFiles(validation.ValidationFiles),
		},
		DiagnosticsJSON: string(diagnosticsJSON), EvidenceJSON: evidence,
		Actor: reviewActor(ctx), NowMS: now,
	})
	if err != nil {
		return fmt.Errorf("commit accepted arcade parent attachment: %w", err)
	}
	return nil
}

func arcadeParentSourceFiles(files []attachedSourceFile) []libraryimportmodel.ArcadeParentSourceFile {
	result := make([]libraryimportmodel.ArcadeParentSourceFile, 0, len(files))
	for _, file := range files {
		var archiveBlobID *string
		if file.archiveBlobID.Valid {
			value := file.archiveBlobID.String
			archiveBlobID = &value
		}
		var archiveOrdinal *int
		if file.archiveOrdinal.Valid {
			value := int(file.archiveOrdinal.Int64)
			archiveOrdinal = &value
		}
		result = append(result, libraryimportmodel.ArcadeParentSourceFile{
			Role: file.role, LogicalName: file.logicalName, UploadFileID: file.uploadFileID,
			BlobID: file.blobID, BlobSHA: file.blobSHA, BlobSize: file.blobSize,
			ArchiveBlobID: archiveBlobID, ArchiveOrdinal: archiveOrdinal, SortOrder: file.sortOrder,
		})
	}
	return result
}

func arcadeParentValidationFiles(files []preparedValidationFile) []libraryimportmodel.ArcadeParentValidationFile {
	result := make([]libraryimportmodel.ArcadeParentValidationFile, 0, len(files))
	for _, file := range files {
		result = append(result, libraryimportmodel.ArcadeParentValidationFile{
			Role: file.Role, LogicalName: file.LogicalName, BlobID: file.BlobID, SortOrder: file.SortOrder,
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
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "archiveCode": archiveCode, "missingEntries": missing,
		"mismatchedEntries": mismatched,
	})
	evidence := marshalReviewEventV2(map[string]any{
		"attachmentKind": "ARCADE_PARENT", "machine": candidate.machine,
		"state": "REJECTED", "errorCode": code,
	})
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	_ = repository.FinishRejected(ctx, libraryimportmodel.ArcadeParentRejectedCommit{
		AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, JobID: jobID, WorkerID: workerID,
		Code: code, DiagnosticsJSON: string(diagnostics), EvidenceJSON: evidence,
		BlobSize: candidate.blobSize, BlobSHA: candidate.blobSHA, Actor: reviewActor(ctx),
		NowMS: service.now().UnixMilli(),
	})
}

func (service *Service) finishRetryableParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code string,
) {
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	_ = repository.FinishRetryable(ctx, libraryimportmodel.ArcadeParentRetryableCommit{
		AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, JobID: jobID, WorkerID: workerID,
		Code: code, DiagnosticsJSON: diagnostics, BlobSize: candidate.blobSize, BlobSHA: candidate.blobSHA,
		NowMS: service.now().UnixMilli(),
	})
}

func (service *Service) SyncParentAttachmentCancellation(ctx context.Context, jobID string) {
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	_ = repository.SyncCancellation(ctx, libraryimportmodel.ArcadeParentCancellationSync{
		JobID: jobID, NowMS: service.now().UnixMilli(),
	})
}

func (service *Service) finishParentAttachmentCancellation(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) bool {
	repository := librarypersistence.NewArcadeParentCommitRepository(service.database)
	ok, _ := repository.FinishCancellation(ctx, libraryimportmodel.ArcadeParentAttachmentCancellation{
		AttachmentID: candidate.attachmentID, ItemID: candidate.itemID, JobID: jobID, WorkerID: workerID,
		NowMS: service.now().UnixMilli(),
	})
	return ok
}
