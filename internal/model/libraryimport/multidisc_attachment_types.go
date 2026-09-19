package libraryimport

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/multidisc"
)

const (
	MultiDiscAttachmentErrorInvalid         = "REVIEW_MULTI_DISC_UPLOAD_INVALID"
	MultiDiscAttachmentErrorNotFound        = "REVIEW_NOT_FOUND"
	MultiDiscAttachmentErrorVersion         = "REVIEW_VERSION_CONFLICT"
	MultiDiscAttachmentErrorInProgress      = "REVIEW_MULTI_DISC_ATTACHMENT_IN_PROGRESS"
	MultiDiscAttachmentErrorRetryRequired   = "REVIEW_MULTI_DISC_ATTACHMENT_RETRY_REQUIRED"
	MultiDiscAttachmentErrorInputStale      = "REVIEW_MULTI_DISC_INPUT_STALE"
	MultiDiscAttachmentErrorFinalized       = "REVIEW_ALREADY_FINALIZED"
	MultiDiscAttachmentErrorContentInvalid  = "REVIEW_MULTI_DISC_CONTENT_INVALID"
	MultiDiscAttachmentErrorSetMismatch     = "REVIEW_MULTI_DISC_ATTACHMENT_SET_MISMATCH"
	MultiDiscAttachmentErrorModeUnavailable = "MULTI_DISC_MODE_UNAVAILABLE"
	MultiDiscAttachmentErrorUnavailable     = "REVIEW_MULTI_DISC_VALIDATION_UNAVAILABLE"
)

type MultiDiscAttachmentError struct {
	Code  string
	Cause error
}

func (value *MultiDiscAttachmentError) Error() string {
	if value.Cause == nil {
		return value.Code
	}
	return fmt.Sprintf("%s: %v", value.Code, value.Cause)
}

func (value *MultiDiscAttachmentError) Unwrap() error { return value.Cause }

func MultiDiscAttachmentErrorCode(err error) string {
	var value *MultiDiscAttachmentError
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

type MultiDiscAttachmentRequest struct {
	UploadID string `json:"uploadId"`
}

type MultiDiscAttachmentCreated struct {
	AttachmentID  string `json:"attachmentId"`
	State         string `json:"state"`
	JobID         string `json:"jobId"`
	ReviewVersion int64  `json:"reviewVersion"`
}

type MultiDiscAttachmentInput struct {
	SchemaVersion        int    `json:"schemaVersion"`
	AttachmentID         string `json:"attachmentId"`
	ImportItemID         string `json:"importItemId"`
	ReviewDraftID        string `json:"reviewDraftId"`
	RequestedByUserID    string `json:"requestedByUserId"`
	BaseSourceSnapshotID string `json:"baseSourceSnapshotId"`
	BaseValidationID     string `json:"baseValidationId"`
	UploadSessionID      string `json:"uploadSessionId"`
	ExpectedSetDigest    string `json:"expectedSetDigest"`
	TargetPlatformID     string `json:"targetPlatformId"`
	PlatformInstanceID   string `json:"platformInstanceId"`
	PlatformVersion      int64  `json:"platformVersion"`
	CoreID               string `json:"coreId"`
	ProviderID           string `json:"providerId"`
	TargetID             string `json:"targetId"`
	ContentPolicyDigest  string `json:"contentPolicyDigest"`
	MaxDiscs             int    `json:"maxDiscs"`
	MaxTotalBytes        int64  `json:"maxTotalBytes"`
}

type MultiDiscAttachmentAdmission struct {
	DraftID, ItemState, SnapshotID, PlatformID, PlatformInstanceID, CoreID  string
	ProviderID, TargetID, ValidationID, ValidationStatus, CompatibilityCode string
	Policy                                                                  contentcapability.Policy
	DraftVersion, PlatformVersion, ValidationPlatformVersion                int64
	ValidationCoreID, ValidationProviderID, ValidationTargetID              string
}
type (
	MultiDiscAttachmentHead   struct{ State, ContentKind string }
	MultiDiscAttachmentUpload struct {
		State, SourceType string
		Consumed          bool
	}
)

type (
	MultiDiscAttachmentActivity struct{ Active, Retryable int64 }
	MultiDiscAttachmentWrite    struct {
		Input                                             MultiDiscAttachmentInput
		JobID, AuditID, InputJSON, InputDigest, DedupeKey string
		RequestVersion, Now                               int64
		AuditJSON                                         string
	}
)

type MultiDiscAttachmentReader interface {
	Admission(context.Context, string) (MultiDiscAttachmentAdmission, bool, error)
	Head(context.Context, string) (MultiDiscAttachmentHead, bool, error)
	Entries(context.Context, string) ([]multidisc.Entry, error)
	Upload(context.Context, string) (MultiDiscAttachmentUpload, bool, error)
	Activity(context.Context, string) (MultiDiscAttachmentActivity, error)
}
type MultiDiscAttachmentQueue interface {
	Job(context.Context, MultiDiscAttachmentWrite) error
	Input(context.Context, MultiDiscAttachmentWrite) error
	Event(context.Context, MultiDiscAttachmentWrite) error
}
type MultiDiscAttachmentReview interface {
	Attachment(context.Context, MultiDiscAttachmentWrite) error
	Draft(context.Context, MultiDiscAttachmentWrite) error
	Audit(context.Context, MultiDiscAttachmentWrite) error
}
type MultiDiscAttachmentScope struct {
	Read   MultiDiscAttachmentReader
	Queue  MultiDiscAttachmentQueue
	Review MultiDiscAttachmentReview
}
type MultiDiscAttachmentRepository interface {
	WithAttachmentAdmission(context.Context, func(MultiDiscAttachmentScope) error) error
}
