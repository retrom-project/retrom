package libraryimport

import (
	"context"
	"errors"
	"time"

	"retrom/internal/cleanup"
	libraryservice "retrom/internal/service/libraryimport"
)

func applicationMultiDiscAttachmentFile(file attachedMultiDiscFile) libraryservice.MultiDiscAttachmentFile {
	return libraryservice.MultiDiscAttachmentFile{
		Role: file.role, LogicalName: file.logicalName, UploadFileID: file.uploadFileID,
		FileRecord: file.fileRecord, BlobSHA: file.blobSHA, BlobSize: file.blobSize, SortOrder: file.sortOrder,
	}
}

func applicationMultiDiscAttachmentTarget(
	candidate multiDiscAttachmentCandidate,
) libraryservice.MultiDiscAttachmentTerminalTarget {
	return libraryservice.MultiDiscAttachmentTerminalTarget{
		AttachmentID: candidate.input.AttachmentID, ItemID: candidate.input.ImportItemID,
		JobID: candidate.jobID, WorkerID: candidate.workerID,
		RequestedByUserID:    candidate.input.RequestedByUserID,
		ExecutionStartedAtMS: candidate.executionStartedAtMS,
	}
}

func (service *Service) commitAcceptedMultiDiscAttachment(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	if err := service.prepareMultiDiscDirectory(ctx, candidate); err != nil {
		return err
	}
	baseFiles := make([]libraryservice.MultiDiscAttachmentFile, 0, len(candidate.baseFiles))
	for _, file := range candidate.baseFiles {
		baseFiles = append(baseFiles, applicationMultiDiscAttachmentFile(file))
	}
	err := service.attachmentCommits.CommitAccepted(
		ctx,
		libraryservice.MultiDiscAttachmentCommitRequest{
			Input: candidate.input, JobID: candidate.jobID, WorkerID: candidate.workerID,
			ExecutionStartedAtMS: candidate.executionStartedAtMS, BaseFiles: baseFiles,
			ResultEntries: candidate.resultEntries, CanonicalPlaylist: candidate.canonicalPlaylist,
			ResultManifestJSON: candidate.resultManifestJSON, ResultManifestDigest: candidate.resultManifestDigest,
		},
	)
	if errors.Is(err, libraryservice.ErrInvalid) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	if err != nil {
		return multiDiscAttachmentStoreError("commit accepted attachment", err)
	}
	return nil
}

func (service *Service) runMultiDiscAttachment(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, multiDiscAttachmentDeadline)
	defer cancel()
	candidate, err := service.claimMultiDiscAttachment(ctx, jobID)
	if err != nil {
		return
	}
	remaining := time.Duration(candidate.deadlineAtMS-service.now().UnixMilli()) * time.Millisecond
	ctx, deadlineCancel := context.WithTimeout(ctx, remaining)
	defer deadlineCancel()
	ctx, leaseCancel := service.attachmentLease(ctx, jobID, candidate.workerID)
	defer leaseCancel()
	if err := service.readAttachedMultiDiscBase(ctx, &candidate); err != nil {
		service.finishRejectedMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorInputStale, err)
		return
	}
	if err := service.readMultiDiscAttachmentUploads(ctx, &candidate); err != nil {
		service.finishRejectedMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorInputStale, err)
		return
	}
	if err := service.validateMultiDiscAttachmentContents(ctx, &candidate); err != nil {
		if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
			return
		}
		code := MultiDiscAttachmentErrorCode(err)
		if code == "" {
			service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, err)
			return
		}
		service.finishRejectedMultiDiscAttachment(ctx, candidate, code, err)
		return
	}
	if err := service.commitAcceptedMultiDiscAttachment(ctx, &candidate); err != nil {
		if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
			return
		}
		code := MultiDiscAttachmentErrorCode(err)
		if code == MultiDiscAttachmentErrorInputStale {
			service.finishRejectedMultiDiscAttachment(ctx, candidate, code, err)
			return
		}
		service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, err)
	}
}

func multiDiscAttachmentActor(ctx context.Context) libraryservice.MultiDiscAttachmentActor {
	actor := reviewActor(ctx)
	userID, _ := actor.UserID.(string)
	label, _ := actor.Label.(string)
	return libraryservice.MultiDiscAttachmentActor{Kind: actor.Kind, UserID: userID, Label: label}
}

func (service *Service) finishRejectedMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	cause error,
) {
	if ctx.Err() != nil {
		service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, cause)
		return
	}
	ctx, cancel := attachmentCleanupContext(ctx)
	defer cancel()
	cleanup.Error("reject multi-disc attachment", service.attachmentTerminals.Reject(
		ctx,
		libraryservice.MultiDiscAttachmentRejectRequest{
			Target: applicationMultiDiscAttachmentTarget(candidate), Actor: multiDiscAttachmentActor(ctx),
			Code: code, Cause: MultiDiscAttachmentErrorCode(cause),
		},
	))
}

func (service *Service) finishRetryableMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	_ error,
) {
	ctx, cancel := attachmentCleanupContext(ctx)
	defer cancel()
	if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
		return
	}
	result, err := service.attachmentTerminals.Retry(
		ctx,
		libraryservice.MultiDiscAttachmentRetryRequest{
			Target: applicationMultiDiscAttachmentTarget(candidate), Code: code,
		},
	)
	cleanup.Error("retry multi-disc attachment", err)
	if err == nil && result.Scheduled {
		service.scheduleMultiDiscAttachmentRun(ctx, candidate.jobID, result.Delay)
	}
}

func (service *Service) finishMultiDiscAttachmentCancellation(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
) bool {
	ctx, cancel := attachmentCleanupContext(ctx)
	defer cancel()
	result, err := service.attachmentTerminals.FinishCancellation(
		ctx,
		libraryservice.MultiDiscAttachmentCancellationRequest{
			Target: applicationMultiDiscAttachmentTarget(candidate),
		},
	)
	cleanup.Error("finish multi-disc attachment cancellation", err)
	return err == nil && result
}
