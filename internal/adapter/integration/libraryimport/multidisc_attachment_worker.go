package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"path"
	"time"

	composition "retrom/internal/bootstrap/composition/libraryimport"
	application "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/capability/content/multidisc"
	"retrom/internal/foundation/cleanup"
)

func (service *Service) ResumeMultiDiscAttachmentJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	for _, job := range service.queuedJobRuns(ctx, "REVIEW_MULTI_DISC_VALIDATE") {
		service.scheduleMultiDiscAttachmentRun(ctx, job.id, time.Duration(job.availableAt-now)*time.Millisecond)
	}
}

func (service *Service) scheduleMultiDiscAttachmentRun(
	ctx context.Context,
	jobID string,
	delay time.Duration,
) {
	workerContext := context.WithoutCancel(ctx)
	if delay <= 0 {
		go service.runMultiDiscAttachment(workerContext, jobID)
		return
	}
	time.AfterFunc(delay, func() { service.runMultiDiscAttachment(workerContext, jobID) })
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
	base, err := composition.NewMultiDiscAttachmentSources(service.database).BaseFiles(
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

func attachedMultiDiscFileFromApplication(file application.MultiDiscAttachmentFile) attachedMultiDiscFile {
	return attachedMultiDiscFile{
		role:         file.Role,
		logicalName:  file.LogicalName,
		uploadFileID: file.UploadFileID,
		blobID:       file.BlobID,
		blobSHA:      file.BlobSHA,
		blobSize:     file.BlobSize,
		sortOrder:    file.SortOrder,
	}
}

func (service *Service) readMultiDiscAttachmentUploads(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	upload, err := composition.NewMultiDiscAttachmentSources(service.database).UploadFiles(
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
	reader, err := service.blobs.OpenDigest(file.blobSHA)
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
		UploadFileID: file.uploadFileID, BlobID: file.blobID, BlobSHA256: file.blobSHA,
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
