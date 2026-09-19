package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/multidisc"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

func (repository *MultiDiscAttachments) CommitAttachmentAdmission(
	ctx context.Context, cmd application.AttachmentAdmissionCommand,
) (application.MultiDiscAttachmentCreated, error) {
	var result application.MultiDiscAttachmentCreated
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := multidiscAdmissionRecords{executor: executor}
		input, err := prepareMultiDiscAdmissionInput(ctx, records, cmd)
		if err != nil {
			return err
		}
		write := application.MultiDiscAttachmentWrite{
			RequestVersion: cmd.ExpectedVersion,
			JobID:          cmd.JobID,
			AuditID:        cmd.AuditID,
		}
		write.Input = input
		write.Now = cmd.NowMS
		if err := encodeMultiDiscAdmissionWrite(&write); err != nil {
			return err
		}
		scope := application.MultiDiscAttachmentScope{Read: records, Queue: records, Review: records}
		if err := persistMultiDiscAdmissionWrite(ctx, scope, write); err != nil {
			return err
		}
		result = application.MultiDiscAttachmentCreated{
			AttachmentID: write.Input.AttachmentID, JobID: write.JobID,
			State: "QUEUED", ReviewVersion: cmd.ExpectedVersion + 1,
		}
		return nil
	})
	if err != nil {
		return application.MultiDiscAttachmentCreated{}, err
	}
	return result, nil
}

func prepareMultiDiscAdmissionInput(
	ctx context.Context,
	records multidiscAdmissionRecords,
	cmd application.AttachmentAdmissionCommand,
) (application.MultiDiscAttachmentInput, error) {
	admission, found, err := records.Admission(ctx, cmd.ItemID)
	if err != nil {
		return application.MultiDiscAttachmentInput{}, fmt.Errorf("read multi-disc admission: %w", err)
	}
	if !found {
		return application.MultiDiscAttachmentInput{}, classifyMissingMultiDiscAdmission(ctx, records, cmd.ItemID)
	}
	if err := validateMultiDiscAdmissionState(admission, cmd.ExpectedVersion); err != nil {
		return application.MultiDiscAttachmentInput{}, err
	}
	capabilities := contentcapability.Resolve(admission.PlatformID, true, true, admission.Policy)
	if capabilities.MultiDisc == nil {
		return application.MultiDiscAttachmentInput{}, &application.MultiDiscAttachmentError{
			Code: application.MultiDiscAttachmentErrorModeUnavailable, Cause: application.ErrInvalid,
		}
	}
	entries, err := records.Entries(ctx, admission.SnapshotID)
	if err != nil {
		return application.MultiDiscAttachmentInput{}, fmt.Errorf("read multi-disc source entries: %w", err)
	}
	if !multiDiscAdmissionHasMissingDiscs(entries) {
		return application.MultiDiscAttachmentInput{}, &application.MultiDiscAttachmentError{
			Code: application.MultiDiscAttachmentErrorContentInvalid, Cause: application.ErrInvalid,
		}
	}
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil {
		return application.MultiDiscAttachmentInput{}, &application.MultiDiscAttachmentError{
			Code: application.MultiDiscAttachmentErrorInputStale, Cause: err,
		}
	}
	if err := validateMultiDiscUploadState(ctx, records, cmd.ItemID, cmd.UploadID); err != nil {
		return application.MultiDiscAttachmentInput{}, err
	}
	return application.MultiDiscAttachmentInput{
		SchemaVersion: 1, ImportItemID: cmd.ItemID, ReviewDraftID: admission.DraftID,
		AttachmentID: cmd.AttachmentID, RequestedByUserID: cmd.ActorUserID,
		BaseSourceSnapshotID: admission.SnapshotID, BaseValidationID: admission.ValidationID,
		UploadSessionID: cmd.UploadID, ExpectedSetDigest: digest,
		TargetPlatformID: admission.PlatformID, PlatformInstanceID: admission.PlatformInstanceID,
		PlatformVersion: admission.PlatformVersion, CoreID: admission.CoreID,
		ProviderID: admission.ProviderID, TargetID: admission.TargetID,
		ContentPolicyDigest: admission.Policy.DigestFor("MULTI_DISC"),
		MaxDiscs: capabilities.MultiDisc.MaxDiscs, MaxTotalBytes: capabilities.MultiDisc.MaxTotalBytes,
	}, nil
}

func classifyMissingMultiDiscAdmission(
	ctx context.Context, records multidiscAdmissionRecords, itemID string,
) error {
	head, found, err := records.Head(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read multi-disc item head: %w", err)
	}
	code := application.MultiDiscAttachmentErrorInputStale
	switch {
	case !found:
		code = application.MultiDiscAttachmentErrorNotFound
	case head.State != "REVIEW_PENDING":
		code = application.MultiDiscAttachmentErrorFinalized
	case head.ContentKind != multidisc.ContentKind:
		code = application.MultiDiscAttachmentErrorModeUnavailable
	}
	return &application.MultiDiscAttachmentError{Code: code, Cause: application.ErrInvalid}
}

func validateMultiDiscAdmissionState(admission application.MultiDiscAttachmentAdmission, expectedVersion int64) error {
	if admission.ItemState != "REVIEW_PENDING" {
		return &application.MultiDiscAttachmentError{Code: application.MultiDiscAttachmentErrorFinalized, Cause: application.ErrInvalid}
	}
	if admission.DraftVersion != expectedVersion {
		return &application.MultiDiscAttachmentError{Code: application.MultiDiscAttachmentErrorVersion, Cause: application.ErrInvalid}
	}
	current := admission.ValidationStatus == "BLOCKED" && admission.CompatibilityCode == "MULTI_DISC_FILE_MISSING" &&
		admission.ValidationCoreID == admission.CoreID && admission.ValidationProviderID == admission.ProviderID &&
		admission.ValidationTargetID == admission.TargetID
	if !current {
		return &application.MultiDiscAttachmentError{Code: application.MultiDiscAttachmentErrorInputStale, Cause: application.ErrInvalid}
	}
	return nil
}

func validateMultiDiscUploadState(
	ctx context.Context, records multidiscAdmissionRecords, itemID, uploadID string,
) error {
	upload, found, err := records.Upload(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("read multi-disc upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.SourceType != "FILES" || upload.Consumed {
		return &application.MultiDiscAttachmentError{Code: application.MultiDiscAttachmentErrorInvalid, Cause: application.ErrInvalid}
	}
	activity, err := records.Activity(ctx, itemID)
	if err != nil {
		return fmt.Errorf("read multi-disc attachment activity: %w", err)
	}
	if activity.Active != 0 {
		return &application.MultiDiscAttachmentError{Code: application.MultiDiscAttachmentErrorInProgress, Cause: application.ErrInvalid}
	}
	if activity.Retryable != 0 {
		return &application.MultiDiscAttachmentError{Code: application.MultiDiscAttachmentErrorRetryRequired, Cause: application.ErrInvalid}
	}
	return nil
}

func encodeMultiDiscAdmissionWrite(write *application.MultiDiscAttachmentWrite) error {
	encoded, err := json.Marshal(write.Input)
	if err != nil {
		return &application.MultiDiscAttachmentError{
			Code: application.MultiDiscAttachmentErrorUnavailable, Cause: err,
		}
	}
	digest := sha256.Sum256(encoded)
	dedupeInput := write.Input.ImportItemID + "\x00" + write.Input.BaseSourceSnapshotID +
		"\x00" + write.Input.ExpectedSetDigest + "\x00" + write.Input.UploadSessionID
	dedupe := sha256.Sum256([]byte(dedupeInput))
	write.InputJSON, write.InputDigest, write.DedupeKey = string(encoded),
		hex.EncodeToString(digest[:]), hex.EncodeToString(dedupe[:])
	write.AuditJSON = `{"schemaVersion":2,"attachmentKind":"MULTI_DISC","state":"QUEUED"}`
	return nil
}

func multiDiscAdmissionHasMissingDiscs(entries []multidisc.Entry) bool {
	for _, entry := range entries {
		if entry.State == multidisc.EntryMissing {
			return true
		}
	}
	return false
}

func persistMultiDiscAdmissionWrite(
	ctx context.Context, scope application.MultiDiscAttachmentScope, write application.MultiDiscAttachmentWrite,
) error {
	for _, save := range []func(context.Context, application.MultiDiscAttachmentWrite) error{
		scope.Queue.Job, scope.Queue.Input, scope.Review.Attachment,
		scope.Queue.Event, scope.Review.Draft, scope.Review.Audit,
	} {
		if err := save(ctx, write); err != nil {
			return err
		}
	}
	return nil
}
