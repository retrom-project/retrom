package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
)

const multiDiscAttachmentDeadline = 30 * time.Minute

const multiDiscAttachmentReadChunk = 8 << 20

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

func multiDiscAttachmentError(code string, cause error) error {
	return &MultiDiscAttachmentError{Code: code, Cause: cause}
}

func MultiDiscAttachmentErrorCode(err error) string {
	var value *MultiDiscAttachmentError
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

func multiDiscAttachmentStoreError(operation string, err error) error {
	return fmt.Errorf("libraryimport/multi-disc attachment %s: %w", operation, err)
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

type multiDiscAttachmentInput struct {
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

type multiDiscAttachmentCandidate struct {
	input                    multiDiscAttachmentInput
	jobID, workerID          string
	executionStartedAtMS     int64
	expectedMissing          []multidisc.Entry
	baseFiles                []attachedMultiDiscFile
	baseEntries              []multidisc.Entry
	resultEntries            []multidisc.Entry
	uploadFiles              []attachedMultiDiscFile
	canonicalPlaylist        blobstore.Metadata
	resultManifestJSON       string
	resultManifestDigest     string
	resultDependencySnapshot corevalidation.Snapshot
	validationStatus         string
	compatibilityCode        string
}

type attachedMultiDiscFile struct {
	role, logicalName, uploadFileID, blobID, blobSHA string
	blobSize                                         int64
	sortOrder                                        int
}

func loadMultiDiscEntries(
	ctx context.Context,
	queryer interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	snapshotID string,
) ([]multidisc.Entry, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT ordinal,source_reference,normalized_reference,canonical_name,state,
upload_file_id,blob_id,source_logical_name
FROM import_item_multidisc_entries
WHERE source_snapshot_id=? ORDER BY ordinal
`, snapshotID)
	if err != nil {
		return nil, multiDiscAttachmentStoreError("read entries", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]multidisc.Entry, 0, multidisc.MaxDiscs)
	for rows.Next() {
		var entry multidisc.Entry
		var state string
		var uploadFileID, blobID, logicalName sql.NullString
		if err := rows.Scan(
			&entry.Ordinal, &entry.SourceReference, &entry.NormalizedReference,
			&entry.CanonicalName, &state, &uploadFileID, &blobID, &logicalName,
		); err != nil {
			return nil, multiDiscAttachmentStoreError("scan entries", err)
		}
		entry.State = multidisc.EntryState(state)
		if entry.State == multidisc.EntryPresent && uploadFileID.Valid && blobID.Valid && logicalName.Valid {
			entry.File = &multidisc.File{
				UploadFileID: uploadFileID.String, BlobID: blobID.String, LogicalName: logicalName.String,
			}
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, multiDiscAttachmentStoreError("iterate entries", err)
	}
	return entries, nil
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

type multiDiscAttachmentAdmission struct {
	draftID, itemState, effectiveSnapshotID, platformID, platformInstanceID, coreID string
	providerID, targetID                                                            string
	contentPolicyJSON, validationID, validationStatus, compatibilityCode            string
	draftVersion, platformVersion, generation                                       int64
	validationPlatformVersion                                                       int64
	validationCoreID, validationProviderID, validationTargetID                      string
}

func (service *Service) readMultiDiscAttachmentAdmission(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
) (multiDiscAttachmentAdmission, error) {
	var admission multiDiscAttachmentAdmission
	err := transaction.QueryRowContext(ctx, `
SELECT draft.id,item.state,draft.version,draft.effective_source_snapshot_id,
platform.platform_id,platform.id,platform.version,platform.default_core_id,
target.provider_id,target.target_id,
json_object(
  'schemaVersion',1,
  'supportedContentKinds',json((SELECT json_group_array(content_kind) FROM (
    SELECT content_kind FROM runtime_binding_content_kinds kinds
    WHERE kinds.binding_id=runtime_binding.binding_id ORDER BY content_kind
  ))),
  'multiDisc',json_object('maxDiscs',8,'maxTotalBytes',1073741824,'delivery','EAGER_EXTERNAL_FILES')
),
validation.id,validation.prepublish_generation,validation.status,validation.compatibility_code,
validation.platform_instance_version,validation.core_id,validation.provider_id,validation.target_id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
AND snapshot.content_kind='MULTI_DISC'
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN runtime_target_bindings runtime_binding ON runtime_binding.core_id=platform.default_core_id
  AND runtime_binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=runtime_binding.binding_id
  AND platform_binding.platform_id=platform.platform_id
JOIN runtime_targets target ON target.provider_id=runtime_binding.provider_id
  AND target.target_id=runtime_binding.target_id
JOIN import_item_core_validations validation ON validation.import_item_id=item.id
AND validation.source_snapshot_id=snapshot.id
AND validation.target_platform_instance_id=platform.id
WHERE item.id=?
ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1
`, itemID).Scan(
		&admission.draftID, &admission.itemState, &admission.draftVersion,
		&admission.effectiveSnapshotID, &admission.platformID, &admission.platformInstanceID,
		&admission.platformVersion, &admission.coreID, &admission.providerID, &admission.targetID,
		&admission.contentPolicyJSON,
		&admission.validationID, &admission.generation,
		&admission.validationStatus, &admission.compatibilityCode,
		&admission.validationPlatformVersion, &admission.validationCoreID,
		&admission.validationProviderID, &admission.validationTargetID,
	)
	if err != nil {
		return multiDiscAttachmentAdmission{}, multiDiscAttachmentStoreError("read admission", err)
	}
	return admission, nil
}

func (service *Service) validateMultiDiscAttachmentUpload(
	ctx context.Context,
	transaction *sql.Tx,
	uploadID string,
) error {
	var state, sourceType string
	var consumed int
	if err := transaction.QueryRowContext(ctx, `
SELECT session.state,session.source_type,EXISTS(
  SELECT 1 FROM upload_consumptions consumption
  WHERE consumption.upload_session_id=session.id AND consumption.upload_file_id IS NULL
)
FROM upload_sessions session WHERE session.id=?
`, uploadID).Scan(&state, &sourceType, &consumed); err != nil ||
		state != "COMPLETE" || sourceType != "FILES" || consumed != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInvalid, err)
	}
	return nil
}

func (service *Service) insertMultiDiscAttachmentJob(
	ctx context.Context,
	transaction *sql.Tx,
	input multiDiscAttachmentInput,
	requestVersion int64,
) (MultiDiscAttachmentCreated, error) {
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	dedupe := sha256.Sum256([]byte(input.ImportItemID + "\x00" + input.BaseSourceSnapshotID + "\x00" +
		input.ExpectedSetDigest + "\x00" + input.UploadSessionID))
	now := service.now().UnixMilli()
	jobID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'IMPORT_ITEM',?,'REVIEW_MULTI_DISC_VALIDATE',?,1,?,1,'QUEUED',0,4,?,?,?)
`, jobID.String(), input.ImportItemID, hex.EncodeToString(dedupe[:]), string(inputJSON), now, now, now); err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	result, err := transaction.ExecContext(ctx, `
INSERT INTO review_multidisc_attachments(id,import_item_id,review_draft_id,requested_by_user_id,
base_source_snapshot_id,upload_session_id,expected_set_digest,state,diagnostics_json,job_id,
version,created_at_ms,updated_at_ms)
SELECT ?,?,?,?,?,?,?,'QUEUED','{}',?,1,?,?
WHERE NOT EXISTS(
  SELECT 1 FROM review_multidisc_attachments active
  WHERE active.import_item_id=? AND active.state IN ('QUEUED','RUNNING','FAILED_RETRYABLE')
)
`, input.AttachmentID, input.ImportItemID, input.ReviewDraftID, input.RequestedByUserID,
		input.BaseSourceSnapshotID, input.UploadSessionID, input.ExpectedSetDigest, jobID.String(), now, now,
		input.ImportItemID)
	if err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorInProgress, ErrInvalid)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{"schemaVersion":1,"state":"QUEUED"}',?)
`, jobID.String(), input.ImportItemID, now); err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_drafts SET version=version+1,updated_at_ms=? WHERE id=? AND version=?
`, now, input.ReviewDraftID, requestVersion)
	if err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorVersion, ErrInvalid)
	}
	eventID, _ := uuid.NewV7()
	evidence := marshalReviewEventV2(map[string]any{"attachmentKind": "MULTI_DISC", "state": "QUEUED"})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_UPLOAD_REQUESTED','USER',?,NULL,?,?,?,?,?,?,?)
`, eventID.String(), input.ImportItemID, input.RequestedByUserID, emptyReviewEventV2, evidence, evidence,
		emptyReviewEventV2, emptyReviewEventV2, emptyReviewEventV2, now); err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	return MultiDiscAttachmentCreated{
		AttachmentID: input.AttachmentID, State: "QUEUED", JobID: jobID.String(), ReviewVersion: requestVersion + 1,
	}, nil
}

func (service *Service) CreateMultiDiscAttachment(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	request MultiDiscAttachmentRequest,
) (MultiDiscAttachmentCreated, error) {
	principal, authenticated := authn.PrincipalFromContext(ctx)
	if expectedVersion < 1 || itemID == "" || request.UploadID == "" || service.blobs == nil ||
		!authenticated || principal.UserID == "" {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorInvalid, ErrInvalid)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	defer cleanup.Rollback(transaction)
	admission, err := service.readMultiDiscAttachmentAdmission(ctx, transaction, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return MultiDiscAttachmentCreated{}, service.classifyMissingMultiDiscAdmission(ctx, transaction, itemID, err)
	}
	if err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	input, err := service.prepareMultiDiscAttachmentInput(
		ctx, transaction, itemID, expectedVersion, request.UploadID, principal.UserID, admission,
	)
	if err != nil {
		return MultiDiscAttachmentCreated{}, err
	}
	created, err := service.insertMultiDiscAttachmentJob(ctx, transaction, input, expectedVersion)
	if err != nil {
		return MultiDiscAttachmentCreated{}, err
	}
	if err := transaction.Commit(); err != nil {
		return MultiDiscAttachmentCreated{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	go service.runMultiDiscAttachment(context.WithoutCancel(ctx), created.JobID)
	return created, nil
}

func (service *Service) classifyMissingMultiDiscAdmission(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	cause error,
) error {
	var itemState, contentKind string
	err := transaction.QueryRowContext(ctx, `
SELECT item.state,snapshot.content_kind
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE item.id=?
`, itemID).Scan(&itemState, &contentKind)
	if errors.Is(err, sql.ErrNoRows) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorNotFound, cause)
	}
	if err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	if itemState != "REVIEW_PENDING" {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorFinalized, cause)
	}
	if contentKind != multidisc.ContentKind {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorModeUnavailable, cause)
	}
	return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, cause)
}

func validateMultiDiscAttachmentAdmission(
	admission multiDiscAttachmentAdmission,
	expectedVersion int64,
) error {
	if admission.itemState != "REVIEW_PENDING" {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorFinalized, ErrInvalid)
	}
	if admission.draftVersion != expectedVersion {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorVersion, ErrInvalid)
	}
	current := admission.generation == prepublishGeneration && admission.validationStatus == "BLOCKED" &&
		admission.compatibilityCode == "MULTI_DISC_FILE_MISSING" &&
		admission.validationPlatformVersion == admission.platformVersion &&
		admission.validationCoreID == admission.coreID && admission.validationProviderID == admission.providerID &&
		admission.validationTargetID == admission.targetID
	if !current {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return nil
}

func multiDiscAttachmentAvailability(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
) error {
	var activeCount, retryCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FILTER(WHERE state IN ('QUEUED','RUNNING')),
       count(*) FILTER(WHERE state='FAILED_RETRYABLE')
FROM review_multidisc_attachments WHERE import_item_id=?
`, itemID).Scan(&activeCount, &retryCount); err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	if activeCount != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInProgress, ErrInvalid)
	}
	if retryCount != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorRetryRequired, ErrInvalid)
	}
	return nil
}

func (service *Service) prepareMultiDiscAttachmentInput(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	expectedVersion int64,
	uploadID, userID string,
	admission multiDiscAttachmentAdmission,
) (multiDiscAttachmentInput, error) {
	if err := validateMultiDiscAttachmentAdmission(admission, expectedVersion); err != nil {
		return multiDiscAttachmentInput{}, err
	}
	capabilities := contentcapability.Resolve(admission.platformID, true, true, admission.contentPolicyJSON)
	if capabilities.MultiDisc == nil {
		return multiDiscAttachmentInput{}, multiDiscAttachmentError(
			MultiDiscAttachmentErrorModeUnavailable, ErrInvalid,
		)
	}
	entries, err := loadMultiDiscEntries(ctx, transaction, admission.effectiveSnapshotID)
	if err != nil {
		return multiDiscAttachmentInput{}, multiDiscAttachmentError(MultiDiscAttachmentErrorUnavailable, err)
	}
	if len(missingMultiDiscEntries(entries)) == 0 {
		return multiDiscAttachmentInput{}, multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, ErrInvalid)
	}
	expectedDigest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil {
		return multiDiscAttachmentInput{}, multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	if err := service.validateMultiDiscAttachmentUpload(ctx, transaction, uploadID); err != nil {
		return multiDiscAttachmentInput{}, err
	}
	if err := multiDiscAttachmentAvailability(ctx, transaction, itemID); err != nil {
		return multiDiscAttachmentInput{}, err
	}
	attachmentID, _ := uuid.NewV7()
	return multiDiscAttachmentInput{
		SchemaVersion: 1, AttachmentID: attachmentID.String(), ImportItemID: itemID,
		ReviewDraftID: admission.draftID, RequestedByUserID: userID,
		BaseSourceSnapshotID: admission.effectiveSnapshotID, BaseValidationID: admission.validationID,
		UploadSessionID: uploadID, ExpectedSetDigest: expectedDigest,
		TargetPlatformID: admission.platformID, PlatformInstanceID: admission.platformInstanceID,
		PlatformVersion: admission.platformVersion, CoreID: admission.coreID,
		ProviderID: admission.providerID, TargetID: admission.targetID,
		ContentPolicyDigest: compatibilityConfigDigest(admission.contentPolicyJSON),
		MaxDiscs:            capabilities.MultiDisc.MaxDiscs, MaxTotalBytes: capabilities.MultiDisc.MaxTotalBytes,
	}, nil
}
