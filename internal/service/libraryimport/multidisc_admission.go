package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/multidisc"
	"retrom/internal/capability/security/authn"
)

type MultiDiscAttachments struct {
	repository model.MultiDiscAttachmentRepository
	options    MultiDiscAttachmentOptions
	newID      func() (string, error)
}

func NewMultiDiscAttachments(
	repository model.MultiDiscAttachmentRepository, options MultiDiscAttachmentOptions,
) *MultiDiscAttachments {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &MultiDiscAttachments{repository: repository, options: options, newID: newImportAdmissionID}
}

func (service *MultiDiscAttachments) Create(
	ctx context.Context,
	itemID string,
	version int64,
	request model.MultiDiscAttachmentRequest,
) (model.MultiDiscAttachmentCreated, error) {
	principal, authenticated := authn.PrincipalFromContext(ctx)
	if version < 1 || version == math.MaxInt64 || itemID == "" || request.UploadID == "" ||
		!service.options.StorageAvailable || !authenticated || principal.UserID == "" {
		return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(
			model.MultiDiscAttachmentErrorInvalid,
			model.ErrInvalid,
		)
	}
	write := model.MultiDiscAttachmentWrite{RequestVersion: version}
	for _, destination := range []*string{&write.Input.AttachmentID, &write.JobID, &write.AuditID} {
		id, err := service.newID()
		if err != nil {
			return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(
				model.MultiDiscAttachmentErrorUnavailable,
				err,
			)
		}
		if id == "" {
			return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(
				model.MultiDiscAttachmentErrorUnavailable,
				model.ErrInvalid,
			)
		}
		*destination = id
	}
	err := service.repository.WithAttachmentAdmission(ctx, func(scope model.MultiDiscAttachmentScope) error {
		prepared, err := prepareMultiDiscInput(ctx, scope.Read, itemID, version, request.UploadID, principal.UserID)
		if err != nil {
			return err
		}
		prepared.AttachmentID = write.Input.AttachmentID
		write.Input = prepared
		write.Now = service.options.Now().UnixMilli()
		if err := encodeMultiDiscAdmission(&write); err != nil {
			return err
		}
		return persistMultiDiscAdmission(ctx, scope, write)
	})
	if err != nil {
		return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(multiDiscAdmissionErrorCode(err), err)
	}
	return model.MultiDiscAttachmentCreated{
		AttachmentID: write.Input.AttachmentID, JobID: write.JobID,
		State: "QUEUED", ReviewVersion: version + 1,
	}, nil
}

func multiDiscAdmissionErrorCode(err error) string {
	if code := model.MultiDiscAttachmentErrorCode(err); code != "" {
		return code
	}
	return model.MultiDiscAttachmentErrorUnavailable
}

func prepareMultiDiscInput(
	ctx context.Context,
	read model.MultiDiscAttachmentReader,
	itemID string,
	version int64,
	uploadID, userID string,
) (model.MultiDiscAttachmentInput, error) {
	admission, found, err := read.Admission(ctx, itemID)
	if err != nil {
		return model.MultiDiscAttachmentInput{}, fmt.Errorf("read multi-disc admission: %w", err)
	}
	if !found {
		return model.MultiDiscAttachmentInput{}, classifyMissingMultiDiscInput(ctx, read, itemID)
	}
	if err := validateMultiDiscAdmission(admission, version); err != nil {
		return model.MultiDiscAttachmentInput{}, err
	}
	capabilities := contentcapability.Resolve(admission.PlatformID, true, true, admission.Policy)
	if capabilities.MultiDisc == nil {
		return model.MultiDiscAttachmentInput{}, multiDiscAttachmentError(
			model.MultiDiscAttachmentErrorModeUnavailable,
			model.ErrInvalid,
		)
	}
	entries, err := read.Entries(ctx, admission.SnapshotID)
	if err != nil {
		return model.MultiDiscAttachmentInput{}, fmt.Errorf("read multi-disc source entries: %w", err)
	}
	if !hasMissingDiscs(entries) {
		return model.MultiDiscAttachmentInput{}, multiDiscAttachmentError(
			model.MultiDiscAttachmentErrorContentInvalid,
			model.ErrInvalid,
		)
	}
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil {
		return model.MultiDiscAttachmentInput{}, multiDiscAttachmentError(model.MultiDiscAttachmentErrorInputStale, err)
	}
	if err := validateMultiDiscUpload(ctx, read, itemID, uploadID); err != nil {
		return model.MultiDiscAttachmentInput{}, err
	}
	return model.MultiDiscAttachmentInput{
		SchemaVersion: 1, ImportItemID: itemID, ReviewDraftID: admission.DraftID, RequestedByUserID: userID,
		BaseSourceSnapshotID: admission.SnapshotID, BaseValidationID: admission.ValidationID, UploadSessionID: uploadID,
		ExpectedSetDigest: digest, TargetPlatformID: admission.PlatformID, PlatformInstanceID: admission.PlatformInstanceID,
		PlatformVersion: admission.PlatformVersion,
		CoreID:          admission.CoreID, ProviderID: admission.ProviderID, TargetID: admission.TargetID,
		ContentPolicyDigest: admission.Policy.DigestFor("MULTI_DISC"), MaxDiscs: capabilities.MultiDisc.MaxDiscs,
		MaxTotalBytes: capabilities.MultiDisc.MaxTotalBytes,
	}, nil
}

func encodeMultiDiscAdmission(write *model.MultiDiscAttachmentWrite) error {
	encoded, err := json.Marshal(write.Input)
	if err != nil {
		return multiDiscAttachmentError(model.MultiDiscAttachmentErrorUnavailable, err)
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

func persistMultiDiscAdmission(
	ctx context.Context, scope model.MultiDiscAttachmentScope, write model.MultiDiscAttachmentWrite,
) error {
	for _, save := range []func(context.Context, model.MultiDiscAttachmentWrite) error{
		scope.Queue.Job, scope.Queue.Input, scope.Review.Attachment,
		scope.Queue.Event, scope.Review.Draft, scope.Review.Audit,
	} {
		if err := save(ctx, write); err != nil {
			return err
		}
	}
	return nil
}
