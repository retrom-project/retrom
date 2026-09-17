package libraryimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/multidisc"
)

func classifyMissingMultiDiscInput(ctx context.Context, read model.MultiDiscAttachmentReader, itemID string) error {
	head, found, err := read.Head(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read multi-disc item head: %w", err)
	}
	code := model.MultiDiscAttachmentErrorInputStale
	switch {
	case !found:
		code = model.MultiDiscAttachmentErrorNotFound
	case head.State != "REVIEW_PENDING":
		code = model.MultiDiscAttachmentErrorFinalized
	case head.ContentKind != multidisc.ContentKind:
		code = model.MultiDiscAttachmentErrorModeUnavailable
	}
	return multiDiscAttachmentError(code, model.ErrInvalid)
}

func validateMultiDiscAdmission(value model.MultiDiscAttachmentAdmission, version int64) error {
	if value.ItemState != "REVIEW_PENDING" {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorFinalized, model.ErrInvalid)
	}
	if value.DraftVersion != version {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorVersion, model.ErrInvalid)
	}
	current := value.ValidationStatus == "BLOCKED" && value.CompatibilityCode == "MULTI_DISC_FILE_MISSING" &&
		value.ValidationCoreID == value.CoreID && value.ValidationProviderID == value.ProviderID &&
		value.ValidationTargetID == value.TargetID
	if !current {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorInputStale, model.ErrInvalid)
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

func validateMultiDiscUpload(ctx context.Context, read model.MultiDiscAttachmentReader, itemID, uploadID string) error {
	upload, found, err := read.Upload(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("read multi-disc upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.SourceType != "FILES" || upload.Consumed {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorInvalid, model.ErrInvalid)
	}
	activity, err := read.Activity(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read multi-disc attachment activity: %w", err)
	}
	if activity.Active != 0 {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorInProgress, model.ErrInvalid)
	}
	if activity.Retryable != 0 {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorRetryRequired, model.ErrInvalid)
	}
	return nil
}
