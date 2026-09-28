package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"time"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/multidisc"
)

func (service *Service) ResumeMultiDiscAttachmentJobs(ctx context.Context) error {
	now := service.now().UnixMilli()
	jobs, err := repository.NewQueuedJobs(service.database).Queued(ctx, "REVIEW_MULTI_DISC_VALIDATE")
	if err != nil {
		return fmt.Errorf("resume multi-disc attachments: %w", err)
	}
	for _, job := range jobs {
		service.scheduleMultiDiscAttachmentRun(ctx, job.ID, time.Duration(job.AvailableAtMS-now)*time.Millisecond)
	}
	return nil
}

func (service *Service) scheduleMultiDiscAttachmentRun(
	ctx context.Context,
	jobID string,
	delay time.Duration,
) {
	service.scheduleAttachment(ctx, delay, func(worker context.Context) {
		service.runMultiDiscAttachment(worker, jobID)
	})
}

func (service *Service) claimMultiDiscAttachment(
	ctx context.Context,
	jobID string,
) (multiDiscAttachmentCandidate, error) {
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	claim, err := repository.NewMultiDiscAttachmentWorker(service.database).Claim(
		ctx, jobID, workerID.String(), now,
	)
	if err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("claim", err)
	}
	return multiDiscAttachmentCandidate{
		input: claim.Input, jobID: claim.JobID, workerID: claim.WorkerID,
		executionStartedAtMS: claim.ExecutionStartedAtMS,
	}, nil
}

func (service *Service) readAttachedMultiDiscBase(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	base, err := service.attachmentSources.BaseFiles(
		ctx, candidate.input.BaseSourceSnapshotID,
	)
	if err != nil {
		return multiDiscAttachmentStoreError("read base files", err)
	}
	for _, file := range base.Files {
		candidate.baseFiles = append(candidate.baseFiles, attachedMultiDiscFileFromApplication(file))
	}
	candidate.baseEntries = base.Entries
	candidate.expectedMissing = missingMultiDiscEntries(base.Entries)
	digest, err := multidisc.ExpectedSetDigest(base.Entries)
	if err != nil || digest != candidate.input.ExpectedSetDigest || len(candidate.expectedMissing) == 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return nil
}

func attachedMultiDiscFileFromApplication(file libraryservice.MultiDiscAttachmentFile) attachedMultiDiscFile {
	return attachedMultiDiscFile{
		role:         file.Role,
		logicalName:  file.LogicalName,
		uploadFileID: file.UploadFileID,
		fileRecord:   file.FileRecord,
		blobSHA:      file.BlobSHA,
		blobSize:     file.BlobSize,
		sortOrder:    file.SortOrder,
	}
}

func (service *Service) readMultiDiscAttachmentUploads(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	upload, err := service.attachmentSources.UploadFiles(
		ctx, candidate.input.UploadSessionID,
	)
	if err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	if upload.State != "COMPLETE" || upload.SourceType != "FILES" || upload.Consumed {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, nil)
	}
	for _, file := range upload.Files {
		candidate.uploadFiles = append(candidate.uploadFiles, attachedMultiDiscFileFromApplication(file))
	}
	return nil
}

func validateMultiDiscAttachmentSet(
	missing []multidisc.Entry,
	uploads []attachedMultiDiscFile,
) error {
	if len(missing) != len(uploads) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
	}
	expected := make(map[string]struct{}, len(missing))
	for _, entry := range missing {
		expected[entry.NormalizedReference] = struct{}{}
	}
	observed := make(map[string]struct{}, len(uploads))
	for _, file := range uploads {
		if path.Base(file.logicalName) != file.logicalName || file.logicalName == "." || file.logicalName == ".." {
			return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
		}
		folded := multidisc.ASCIIFold(file.logicalName)
		if _, duplicate := observed[folded]; duplicate {
			return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
		}
		observed[folded] = struct{}{}
	}
	for name := range expected {
		if _, exists := observed[name]; !exists {
			return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
		}
	}
	return nil
}

func (service *Service) heartbeatMultiDiscAttachment(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	if err := ctx.Err(); err != nil {
		return multiDiscAttachmentStoreError("worker context", err)
	}
	now := service.now().UnixMilli()
	if err := repository.NewMultiDiscAttachmentWorker(service.database).Heartbeat(
		ctx, candidate.jobID, candidate.workerID, now,
	); err != nil {
		return multiDiscAttachmentStoreError("heartbeat", err)
	}
	return nil
}

func (service *Service) multiDiscFileForValidation(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
	file attachedMultiDiscFile,
) (multidisc.File, error) {
	if file.blobSize < 8 {
		return multidisc.File{}, multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, ErrInvalid)
	}
	if err := service.heartbeatMultiDiscAttachment(ctx, candidate); err != nil {
		return multidisc.File{}, err
	}
	reader, err := service.blobs.OpenRecord(file.fileRecord)
	if err != nil {
		return multidisc.File{}, multiDiscAttachmentStoreError("open disc", err)
	}
	digest := sha256.New()
	buffer := make([]byte, multiDiscAttachmentReadChunk)
	header := make([]byte, 0, 8)
	var total int64
	for {
		read, readErr := reader.Read(buffer)
		if read > 0 {
			appendMultiDiscDigestChunk(digest, &header, buffer[:read])
			total += int64(read)
			if err := service.heartbeatMultiDiscAttachment(ctx, candidate); err != nil {
				cleanup.Error("close", reader.Close())
				return multidisc.File{}, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			closeErr := reader.Close()
			return multidisc.File{}, multiDiscAttachmentStoreError("read disc", errors.Join(readErr, closeErr))
		}
	}
	if err := reader.Close(); err != nil {
		return multidisc.File{}, multiDiscAttachmentStoreError("close disc", err)
	}
	if total != file.blobSize || hex.EncodeToString(digest.Sum(nil)) != file.blobSHA {
		return multidisc.File{}, multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, ErrInvalid)
	}
	return multidisc.File{
		Basename: file.logicalName, LogicalName: file.logicalName,
		UploadFileID: file.uploadFileID, FileRecord: file.fileRecord, BlobSHA256: file.blobSHA,
		SizeBytes: file.blobSize, Header: header,
	}, nil
}

func appendMultiDiscDigestChunk(digest hash.Hash, header *[]byte, chunk []byte) {
	_, _ = digest.Write(chunk)
	if len(*header) >= cap(*header) {
		return
	}
	needed := cap(*header) - len(*header)
	if needed > len(chunk) {
		needed = len(chunk)
	}
	*header = append(*header, chunk[:needed]...)
}
