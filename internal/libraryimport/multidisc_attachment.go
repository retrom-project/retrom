package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
	"retrom/internal/multidisc"
	libraryservice "retrom/internal/service/libraryimport"
)

const (
	multiDiscAttachmentDeadline  = libraryservice.MultiDiscAttachmentDeadline
	multiDiscAttachmentReadChunk = libraryservice.MultiDiscAttachmentReadChunk
)

const (
	MultiDiscAttachmentErrorInvalid         = libraryservice.MultiDiscAttachmentErrorInvalid
	MultiDiscAttachmentErrorNotFound        = libraryservice.MultiDiscAttachmentErrorNotFound
	MultiDiscAttachmentErrorVersion         = libraryservice.MultiDiscAttachmentErrorVersion
	MultiDiscAttachmentErrorInProgress      = libraryservice.MultiDiscAttachmentErrorInProgress
	MultiDiscAttachmentErrorRetryRequired   = libraryservice.MultiDiscAttachmentErrorRetryRequired
	MultiDiscAttachmentErrorInputStale      = libraryservice.MultiDiscAttachmentErrorInputStale
	MultiDiscAttachmentErrorFinalized       = libraryservice.MultiDiscAttachmentErrorFinalized
	MultiDiscAttachmentErrorContentInvalid  = libraryservice.MultiDiscAttachmentErrorContentInvalid
	MultiDiscAttachmentErrorSetMismatch     = libraryservice.MultiDiscAttachmentErrorSetMismatch
	MultiDiscAttachmentErrorModeUnavailable = libraryservice.MultiDiscAttachmentErrorModeUnavailable
	MultiDiscAttachmentErrorUnavailable     = libraryservice.MultiDiscAttachmentErrorUnavailable
)

type (
	MultiDiscAttachmentError   = libraryservice.MultiDiscAttachmentError
	MultiDiscAttachmentRequest = libraryservice.MultiDiscAttachmentRequest
	MultiDiscAttachmentCreated = libraryservice.MultiDiscAttachmentCreated
	multiDiscAttachmentInput   = libraryservice.MultiDiscAttachmentInput
)

func multiDiscAttachmentError(code string, cause error) error {
	return &MultiDiscAttachmentError{Code: code, Cause: cause}
}

func MultiDiscAttachmentErrorCode(err error) string {
	return libraryservice.MultiDiscAttachmentErrorCode(err)
}

func multiDiscAttachmentStoreError(operation string, err error) error {
	return fmt.Errorf("multi-disc attachment %s: %w", operation, err)
}

type multiDiscAttachmentCandidate struct {
	input                multiDiscAttachmentInput
	jobID, workerID      string
	executionStartedAtMS int64
	deadlineAtMS         int64
	expectedMissing      []multidisc.Entry
	baseFiles            []attachedMultiDiscFile
	baseEntries          []multidisc.Entry
	resultEntries        []multidisc.Entry
	uploadFiles          []attachedMultiDiscFile
	canonicalPlaylist    filestore.Metadata
	resultManifestJSON   string
	resultManifestDigest string
}

type attachedMultiDiscFile struct {
	role, logicalName, uploadFileID, fileRecord, blobSHA string
	blobSize                                             int64
	sortOrder                                            int
}

func missingMultiDiscEntries(entries []multidisc.Entry) []multidisc.Entry {
	missing := make([]multidisc.Entry, 0)
	for _, entry := range entries {
		if entry.State == multidisc.EntryMissing {
			missing = append(missing, entry)
		}
	}
	return missing
}

func (service *Service) CreateMultiDiscAttachment(
	ctx context.Context,
	itemID string,
	version int64,
	request MultiDiscAttachmentRequest,
) (MultiDiscAttachmentCreated, error) {
	result, err := service.attachmentCreator.Create(ctx, itemID, version, request)
	if err != nil {
		return MultiDiscAttachmentCreated{}, fmt.Errorf("create multi-disc attachment: %w", err)
	}
	service.scheduleMultiDiscAttachmentRun(ctx, result.JobID, 0)
	return result, nil
}
