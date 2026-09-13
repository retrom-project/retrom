package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"retrom/internal/authn"
	"retrom/internal/contentcapability"
	"retrom/internal/multidisc"
)

type MultiDiscAttachments struct {
	repository MultiDiscAttachmentRepository
	options    MultiDiscAttachmentOptions
	newID      func() (string, error)
}

func NewMultiDiscAttachments(repository MultiDiscAttachmentRepository, options MultiDiscAttachmentOptions) *MultiDiscAttachments {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &MultiDiscAttachments{repository: repository, options: options, newID: newImportAdmissionID}
}

func (service *MultiDiscAttachments) Create(ctx context.Context, itemID string, version int64, request MultiDiscAttachmentRequest) (MultiDiscAttachmentCreated, error) {
	principal, authenticated := authn.PrincipalFromContext(ctx)
	if version < 1 || version == math.MaxInt64 || itemID == "" || request.UploadID == "" || !service.options.StorageAvailable || !authenticated || principal.UserID == "" {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorInvalid, ErrInvalid)
	}
	write := MultiDiscAttachmentWrite{RequestVersion: version}
	for _, destination := range []*string{&write.Input.AttachmentID, &write.JobID, &write.AuditID} {
		id, err := service.newID()
		if err != nil {
			return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
		}
		if id == "" {
			return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, ErrInvalid)
		}
		*destination = id
	}
	err := service.repository.WithAttachmentAdmission(ctx, func(scope MultiDiscAttachmentScope) error {
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
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(multiDiscAdmissionErrorCode(err), err)
	}
	return MultiDiscAttachmentCreated{AttachmentID: write.Input.AttachmentID, JobID: write.JobID, State: "QUEUED", ReviewVersion: version + 1}, nil
}

func multiDiscAdmissionErrorCode(err error) string {
	if code := MultiDiscAttachmentErrorCode(err); code != "" {
		return code
	}
	return MultiDiscAttachmentErrorUnavailable
}

func prepareMultiDiscInput(ctx context.Context, read MultiDiscAttachmentReader, itemID string, version int64, uploadID, userID string) (MultiDiscAttachmentInput, error) {
	admission, found, err := read.Admission(ctx, itemID)
	if err != nil {
		return MultiDiscAttachmentInput{}, err
	}
	if !found {
		return MultiDiscAttachmentInput{}, classifyMissingMultiDiscInput(ctx, read, itemID)
	}
	if err := validateMultiDiscAdmission(admission, version); err != nil {
		return MultiDiscAttachmentInput{}, err
	}
	capabilities := contentcapability.Resolve(admission.PlatformID, true, true, admission.Policy)
	if capabilities.MultiDisc == nil {
		return MultiDiscAttachmentInput{}, multiDiscAttachmentError(MultiDiscAttachmentErrorModeUnavailable, ErrInvalid)
	}
	entries, err := read.Entries(ctx, admission.SnapshotID)
	if err != nil {
		return MultiDiscAttachmentInput{}, err
	}
	if !hasMissingDiscs(entries) {
		return MultiDiscAttachmentInput{}, multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, ErrInvalid)
	}
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil {
		return MultiDiscAttachmentInput{}, multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	if err := validateMultiDiscUpload(ctx, read, itemID, uploadID); err != nil {
		return MultiDiscAttachmentInput{}, err
	}
	return MultiDiscAttachmentInput{
		SchemaVersion: 1, ImportItemID: itemID, ReviewDraftID: admission.DraftID, RequestedByUserID: userID,
		BaseSourceSnapshotID: admission.SnapshotID, BaseValidationID: admission.ValidationID, UploadSessionID: uploadID,
		ExpectedSetDigest: digest, TargetPlatformID: admission.PlatformID, PlatformInstanceID: admission.PlatformInstanceID,
		PlatformVersion: admission.PlatformVersion, CoreID: admission.CoreID, ProviderID: admission.ProviderID, TargetID: admission.TargetID,
		ContentPolicyDigest: admission.Policy.DigestFor("MULTI_DISC"), MaxDiscs: capabilities.MultiDisc.MaxDiscs,
		MaxTotalBytes: capabilities.MultiDisc.MaxTotalBytes,
	}, nil
}

func encodeMultiDiscAdmission(write *MultiDiscAttachmentWrite) error {
	encoded, err := json.Marshal(write.Input)
	if err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	digest := sha256.Sum256(encoded)
	dedupe := sha256.Sum256([]byte(write.Input.ImportItemID + "\x00" + write.Input.BaseSourceSnapshotID + "\x00" + write.Input.ExpectedSetDigest + "\x00" + write.Input.UploadSessionID))
	write.InputJSON, write.InputDigest, write.DedupeKey = string(encoded), hex.EncodeToString(digest[:]), hex.EncodeToString(dedupe[:])
	write.AuditJSON = `{"schemaVersion":2,"attachmentKind":"MULTI_DISC","state":"QUEUED"}`
	return nil
}

func persistMultiDiscAdmission(ctx context.Context, scope MultiDiscAttachmentScope, write MultiDiscAttachmentWrite) error {
	for _, save := range []func(context.Context, MultiDiscAttachmentWrite) error{
		scope.Queue.Job, scope.Queue.Input, scope.Review.Attachment, scope.Queue.Event, scope.Review.Draft, scope.Review.Audit,
	} {
		if err := save(ctx, write); err != nil {
			return err
		}
	}
	return nil
}
