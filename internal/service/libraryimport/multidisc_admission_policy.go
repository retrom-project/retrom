package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/multidisc"
)

func classifyMissingMultiDiscInput(ctx context.Context, read MultiDiscAttachmentReader, itemID string) error {
	head, found, err := read.Head(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read multi-disc item head: %w", err)
	}
	code := MultiDiscAttachmentErrorInputStale
	switch {
	case !found:
		code = MultiDiscAttachmentErrorNotFound
	case head.State != "REVIEW_PENDING":
		code = MultiDiscAttachmentErrorFinalized
	case head.ContentKind != multidisc.ContentKind:
		code = MultiDiscAttachmentErrorModeUnavailable
	}
	return multiDiscAttachmentError(code, ErrInvalid)
}

func validateMultiDiscAdmission(value MultiDiscAttachmentAdmission, version int64) error {
	if value.ItemState != "REVIEW_PENDING" {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorFinalized, ErrInvalid)
	}
	if value.DraftVersion != version {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorVersion, ErrInvalid)
	}
	current := value.ValidationStatus == "BLOCKED" && value.CompatibilityCode == "MULTI_DISC_FILE_MISSING" &&
		value.ValidationCoreID == value.CoreID && value.ValidationProviderID == value.ProviderID &&
		value.ValidationTargetID == value.TargetID
	if !current {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return nil
}

func hasMissingDiscs(entries []multidisc.Entry) bool {
	for _, entry := range entries {
		if entry.State == multidisc.EntryMissing {
			return true
		}
	}
	return false
}

func validateMultiDiscUpload(ctx context.Context, read MultiDiscAttachmentReader, itemID, uploadID string) error {
	upload, found, err := read.Upload(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("read multi-disc upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.SourceType != "FILES" || upload.Consumed {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInvalid, ErrInvalid)
	}
	activity, err := read.Activity(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read multi-disc attachment activity: %w", err)
	}
	if activity.Active != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInProgress, ErrInvalid)
	}
	if activity.Retryable != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorRetryRequired, ErrInvalid)
	}
	return nil
}
