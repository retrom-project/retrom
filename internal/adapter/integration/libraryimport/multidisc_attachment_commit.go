package libraryimport

import (
	"context"
	"errors"

	composition "retrom/internal/bootstrap/composition/libraryimport"
	libraryimportmodel "retrom/internal/model/libraryimport"
)

func applicationMultiDiscAttachmentFile(file attachedMultiDiscFile) libraryimportmodel.MultiDiscAttachmentFile {
	return libraryimportmodel.MultiDiscAttachmentFile{
		Role: file.role, LogicalName: file.logicalName, UploadFileID: file.uploadFileID,
		BlobID: file.blobID, BlobSHA: file.blobSHA, BlobSize: file.blobSize, SortOrder: file.sortOrder,
	}
}

func applicationMultiDiscAttachmentTarget(
	candidate multiDiscAttachmentCandidate,
) libraryimportmodel.MultiDiscAttachmentTerminalTarget {
	return libraryimportmodel.MultiDiscAttachmentTerminalTarget{
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
	baseFiles := make([]libraryimportmodel.MultiDiscAttachmentFile, 0, len(candidate.baseFiles))
	for _, file := range candidate.baseFiles {
		baseFiles = append(baseFiles, applicationMultiDiscAttachmentFile(file))
	}
	err := composition.NewMultiDiscAttachmentCommits(service.database, service.now).CommitAccepted(
		ctx, libraryimportmodel.MultiDiscAttachmentCommitRequest{
			Input: candidate.input, JobID: candidate.jobID, WorkerID: candidate.workerID,
			ExecutionStartedAtMS: candidate.executionStartedAtMS, BaseFiles: baseFiles,
			ResultEntries: candidate.resultEntries, CanonicalPlaylist: candidate.canonicalPlaylist,
			ResultManifestJSON: candidate.resultManifestJSON, ResultManifestDigest: candidate.resultManifestDigest,
		},
	)
	if errors.Is(err, libraryimportmodel.ErrInvalid) {
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

func multiDiscAttachmentActor(ctx context.Context) libraryimportmodel.MultiDiscAttachmentActor {
	actor := reviewActor(ctx)
	userID, _ := actor.UserID.(string)
	label, _ := actor.Label.(string)
	return libraryimportmodel.MultiDiscAttachmentActor{Kind: actor.Kind, UserID: userID, Label: label}
}

func (service *Service) finishRejectedMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	cause error,
) {
	_ = composition.NewMultiDiscAttachmentTerminals(service.database, service.now).Reject(
		ctx, libraryimportmodel.MultiDiscAttachmentRejectRequest{
			Target: applicationMultiDiscAttachmentTarget(candidate), Actor: multiDiscAttachmentActor(ctx),
			Code: code, Cause: MultiDiscAttachmentErrorCode(cause),
		},
	)
}

func (service *Service) finishRetryableMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	_ error,
) {
	result, err := composition.NewMultiDiscAttachmentTerminals(service.database, service.now).Retry(
		ctx, libraryimportmodel.MultiDiscAttachmentRetryRequest{
			Target: applicationMultiDiscAttachmentTarget(candidate), Code: code,
		},
	)
	if err == nil && result.Scheduled {
		service.scheduleMultiDiscAttachmentRun(ctx, candidate.jobID, result.Delay)
	}
}

func (service *Service) SyncMultiDiscAttachmentCancellation(ctx context.Context, jobID string) {
	_ = composition.NewMultiDiscAttachmentTerminals(service.database, service.now).SyncCancellation(ctx, jobID)
}

func (service *Service) finishMultiDiscAttachmentCancellation(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
) bool {
	result, err := composition.NewMultiDiscAttachmentTerminals(service.database, service.now).FinishCancellation(
		ctx, libraryimportmodel.MultiDiscAttachmentCancellationRequest{
			Target: applicationMultiDiscAttachmentTarget(candidate),
		},
	)
	return err == nil && result
}
