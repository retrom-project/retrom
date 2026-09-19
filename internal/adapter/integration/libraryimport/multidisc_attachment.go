package libraryimport

import (
	"context"
	"fmt"

	blobmodel "retrom/internal/model/blob"
	libraryimportmodel "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/multidisc"
	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

const (
	multiDiscAttachmentDeadline  = libraryimportmodel.MultiDiscAttachmentDeadline
	multiDiscAttachmentReadChunk = libraryimportmodel.MultiDiscAttachmentReadChunk
)

const (
	MultiDiscAttachmentErrorInvalid         = libraryimportmodel.MultiDiscAttachmentErrorInvalid
	MultiDiscAttachmentErrorNotFound        = libraryimportmodel.MultiDiscAttachmentErrorNotFound
	MultiDiscAttachmentErrorVersion         = libraryimportmodel.MultiDiscAttachmentErrorVersion
	MultiDiscAttachmentErrorInProgress      = libraryimportmodel.MultiDiscAttachmentErrorInProgress
	MultiDiscAttachmentErrorRetryRequired   = libraryimportmodel.MultiDiscAttachmentErrorRetryRequired
	MultiDiscAttachmentErrorInputStale      = libraryimportmodel.MultiDiscAttachmentErrorInputStale
	MultiDiscAttachmentErrorFinalized       = libraryimportmodel.MultiDiscAttachmentErrorFinalized
	MultiDiscAttachmentErrorContentInvalid  = libraryimportmodel.MultiDiscAttachmentErrorContentInvalid
	MultiDiscAttachmentErrorSetMismatch     = libraryimportmodel.MultiDiscAttachmentErrorSetMismatch
	MultiDiscAttachmentErrorModeUnavailable = libraryimportmodel.MultiDiscAttachmentErrorModeUnavailable
	MultiDiscAttachmentErrorUnavailable     = libraryimportmodel.MultiDiscAttachmentErrorUnavailable
)

type (
	MultiDiscAttachmentError   = libraryimportmodel.MultiDiscAttachmentError
	MultiDiscAttachmentRequest = libraryimportmodel.MultiDiscAttachmentRequest
	MultiDiscAttachmentCreated = libraryimportmodel.MultiDiscAttachmentCreated
	multiDiscAttachmentInput   = libraryimportmodel.MultiDiscAttachmentInput
)

func multiDiscAttachmentError(code string, cause error) error {
	return &MultiDiscAttachmentError{Code: code, Cause: cause}
}

func MultiDiscAttachmentErrorCode(err error) string {
	return libraryimportmodel.MultiDiscAttachmentErrorCode(err)
}

func multiDiscAttachmentStoreError(operation string, err error) error {
	return fmt.Errorf("multi-disc attachment %s: %w", operation, err)
}

type multiDiscAttachmentCandidate struct {
	input                multiDiscAttachmentInput
	jobID, workerID      string
	executionStartedAtMS int64
	expectedMissing      []multidisc.Entry
	baseFiles            []attachedMultiDiscFile
	baseEntries          []multidisc.Entry
	resultEntries        []multidisc.Entry
	uploadFiles          []attachedMultiDiscFile
	canonicalPlaylist    blobmodel.PreparedBlob
	resultManifestJSON   string
	resultManifestDigest string
}

type attachedMultiDiscFile struct {
	role, logicalName, uploadFileID, blobID, blobSHA string
	blobSize                                         int64
	sortOrder                                        int
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
	attachments := application.NewMultiDiscAttachments(
		repository.NewMultiDiscAttachments(service.database), application.MultiDiscAttachmentOptions{
			Now: service.now, StorageAvailable: service.blobs != nil,
		},
	)
	result, err := attachments.Create(ctx, itemID, version, request)
	if err != nil {
		return MultiDiscAttachmentCreated{}, fmt.Errorf("create multi-disc attachment: %w", err)
	}
	go service.runMultiDiscAttachment(context.WithoutCancel(ctx), result.JobID)
	return result, nil
}
