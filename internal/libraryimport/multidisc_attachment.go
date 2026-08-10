package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentmanifest"
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
	CoreArtifactID       string `json:"coreArtifactId"`
	CoreArtifactVersion  int64  `json:"coreArtifactVersion"`
	CompatibilityDigest  string `json:"compatibilityConfigDigest"`
	MaxDiscs             int    `json:"maxDiscs"`
	MaxTotalBytes        int64  `json:"maxTotalBytes"`
}

type multiDiscAttachmentCandidate struct {
	input                    multiDiscAttachmentInput
	jobID, workerID          string
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
	draftID, itemState, effectiveSnapshotID, platformID, targetID, coreID string
	artifactID, compatibilityConfig, validationID                         string
	validationStatus, compatibilityCode                                   string
	draftVersion, platformVersion, artifactVersion, generation            int64
	validationPlatformVersion, validationArtifactVersion                  int64
	validationCoreID, validationArtifactID                                string
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
artifact.id,artifact.version,artifact.compatibility_config_json,
validation.id,validation.prepublish_generation,validation.status,validation.compatibility_code,
validation.platform_instance_version,validation.core_id,validation.core_artifact_id,
validation.core_artifact_version
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
AND snapshot.content_kind='MULTI_DISC_M3U_V1'
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.enabled=1
JOIN import_item_core_validations validation ON validation.import_item_id=item.id
AND validation.source_snapshot_id=snapshot.id
AND validation.target_platform_instance_id=platform.id
WHERE item.id=?
ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1
`, itemID).Scan(
		&admission.draftID, &admission.itemState, &admission.draftVersion,
		&admission.effectiveSnapshotID, &admission.platformID, &admission.targetID,
		&admission.platformVersion, &admission.coreID, &admission.artifactID,
		&admission.artifactVersion, &admission.compatibilityConfig,
		&admission.validationID, &admission.generation,
		&admission.validationStatus, &admission.compatibilityCode,
		&admission.validationPlatformVersion, &admission.validationCoreID,
		&admission.validationArtifactID, &admission.validationArtifactVersion,
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
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{}',?)
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
	evidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": input.AttachmentID,
		"baseSourceSnapshotId": input.BaseSourceSnapshotID, "uploadId": input.UploadSessionID,
		"expectedSetDigest": input.ExpectedSetDigest, "state": "QUEUED",
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_UPLOAD_REQUESTED','USER',?,NULL,'{}',?,?,'{}','{}','{}',?)
`, eventID.String(), input.ImportItemID, input.RequestedByUserID, string(evidence), string(evidence), now); err != nil {
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
		admission.validationCoreID == admission.coreID && admission.validationArtifactID == admission.artifactID &&
		admission.validationArtifactVersion == admission.artifactVersion
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
	capabilities := contentcapability.Resolve(admission.platformID, true, true, admission.compatibilityConfig)
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
		TargetPlatformID: admission.platformID, PlatformInstanceID: admission.targetID,
		PlatformVersion: admission.platformVersion, CoreID: admission.coreID,
		CoreArtifactID: admission.artifactID, CoreArtifactVersion: admission.artifactVersion,
		CompatibilityDigest: compatibilityConfigDigest(admission.compatibilityConfig),
		MaxDiscs:            capabilities.MultiDisc.MaxDiscs, MaxTotalBytes: capabilities.MultiDisc.MaxTotalBytes,
	}, nil
}

func (service *Service) ResumeMultiDiscAttachmentJobs(ctx context.Context) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,available_at_ms FROM jobs
WHERE kind='REVIEW_MULTI_DISC_VALIDATE' AND state='QUEUED'
ORDER BY available_at_ms,id
`)
	if err != nil {
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type queuedJob struct {
		id          string
		availableAt int64
	}
	queued := make([]queuedJob, 0)
	for rows.Next() {
		var job queuedJob
		if rows.Scan(&job.id, &job.availableAt) == nil {
			queued = append(queued, job)
		}
	}
	if rows.Err() != nil {
		return
	}
	now := service.now().UnixMilli()
	for _, job := range queued {
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
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("begin claim", err)
	}
	defer cleanup.Rollback(transaction)
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	if err := claimMultiDiscAttachmentRecords(ctx, transaction, jobID, workerID.String(), now); err != nil {
		return multiDiscAttachmentCandidate{}, err
	}
	candidate, err := readClaimedMultiDiscAttachment(ctx, transaction, jobID)
	if err != nil {
		return multiDiscAttachmentCandidate{}, err
	}
	candidate.jobID, candidate.workerID = jobID, workerID.String()
	if err := transaction.Commit(); err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("commit claim", err)
	}
	return candidate, nil
}

func claimMultiDiscAttachmentRecords(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, workerID string,
	now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_MULTI_DISC_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
AND attempt_count<max_attempts
`, workerID, now, now+int64(multiDiscAttachmentDeadline/time.Millisecond),
		now+60_000, now, now, jobID, now)
	if err != nil {
		return multiDiscAttachmentStoreError("claim job", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments
SET state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')
	`, now, jobID)
	if err != nil {
		return multiDiscAttachmentStoreError("mark attachment running", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
	SELECT id,scope_type,scope_id,'STARTED','{}',? FROM jobs WHERE id=?
	`, now, jobID); err != nil {
		return multiDiscAttachmentStoreError("record start", err)
	}
	return nil
}

func readClaimedMultiDiscAttachment(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
) (multiDiscAttachmentCandidate, error) {
	var inputJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT input.input_json
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
	`, jobID).Scan(&inputJSON); err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("read frozen input", err)
	}
	var candidate multiDiscAttachmentCandidate
	if err := json.Unmarshal([]byte(inputJSON), &candidate.input); err != nil ||
		!validMultiDiscAttachmentInput(candidate.input) {
		return multiDiscAttachmentCandidate{}, ErrInvalid
	}
	var attachmentID, itemID, draftID, userID, baseSnapshotID, uploadID, expectedDigest, state string
	if err := transaction.QueryRowContext(ctx, `
SELECT id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,
upload_session_id,expected_set_digest,state
FROM review_multidisc_attachments WHERE job_id=?
`, jobID).Scan(
		&attachmentID, &itemID, &draftID, &userID, &baseSnapshotID,
		&uploadID, &expectedDigest, &state,
	); err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("read claimed attachment", err)
	}
	matches := state == "RUNNING" && attachmentID == candidate.input.AttachmentID &&
		itemID == candidate.input.ImportItemID && draftID == candidate.input.ReviewDraftID &&
		userID == candidate.input.RequestedByUserID && baseSnapshotID == candidate.input.BaseSourceSnapshotID &&
		uploadID == candidate.input.UploadSessionID && expectedDigest == candidate.input.ExpectedSetDigest
	if !matches {
		return multiDiscAttachmentCandidate{}, ErrInvalid
	}
	return candidate, nil
}

func validMultiDiscAttachmentInput(input multiDiscAttachmentInput) bool {
	return input.SchemaVersion == 1 && input.AttachmentID != "" && input.ImportItemID != "" &&
		input.RequestedByUserID != "" && input.MaxDiscs >= multidisc.MinDiscs &&
		input.MaxDiscs <= multidisc.MaxDiscs && input.MaxTotalBytes >= 1 &&
		len(input.CompatibilityDigest) == sha256.Size*2
}

func (service *Service) readAttachedMultiDiscBase(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.blob_id,blob.sha256,blob.size_bytes,file.sort_order
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.source_snapshot_id=? AND file.role IN ('PLAYLIST_SOURCE','DISC')
ORDER BY file.role,file.sort_order
`, candidate.input.BaseSourceSnapshotID)
	if err != nil {
		return multiDiscAttachmentStoreError("read base files", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var file attachedMultiDiscFile
		if err := rows.Scan(
			&file.role, &file.logicalName, &file.uploadFileID, &file.blobID,
			&file.blobSHA, &file.blobSize, &file.sortOrder,
		); err != nil {
			return multiDiscAttachmentStoreError("scan base files", err)
		}
		candidate.baseFiles = append(candidate.baseFiles, file)
	}
	if err := rows.Err(); err != nil {
		return multiDiscAttachmentStoreError("iterate base files", err)
	}
	entries, err := loadMultiDiscEntries(ctx, service.database, candidate.input.BaseSourceSnapshotID)
	if err != nil {
		return err
	}
	candidate.baseEntries = entries
	candidate.expectedMissing = missingMultiDiscEntries(entries)
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil || digest != candidate.input.ExpectedSetDigest || len(candidate.expectedMissing) == 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return nil
}

func (service *Service) readMultiDiscAttachmentUploads(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	var state, sourceType string
	var consumed int
	if err := service.database.QueryRowContext(ctx, `
SELECT state,source_type,EXISTS(
  SELECT 1 FROM upload_consumptions consumption
  WHERE consumption.upload_session_id=upload_sessions.id AND consumption.upload_file_id IS NULL
)
FROM upload_sessions WHERE id=?
`, candidate.input.UploadSessionID).Scan(&state, &sourceType, &consumed); err != nil ||
		state != "COMPLETE" || sourceType != "FILES" || consumed != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.relative_path,file.id,file.final_blob_id,blob.sha256,blob.size_bytes
FROM upload_files file
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.upload_session_id=? AND file.state='COMPLETE'
ORDER BY file.relative_path,file.id
`, candidate.input.UploadSessionID)
	if err != nil {
		return multiDiscAttachmentStoreError("read uploads", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var file attachedMultiDiscFile
		if err := rows.Scan(
			&file.logicalName, &file.uploadFileID, &file.blobID, &file.blobSHA, &file.blobSize,
		); err != nil {
			return multiDiscAttachmentStoreError("scan uploads", err)
		}
		file.role = "DISC"
		candidate.uploadFiles = append(candidate.uploadFiles, file)
	}
	if err := rows.Err(); err != nil {
		return multiDiscAttachmentStoreError("iterate uploads", err)
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
	if err := expectOneRow(service.database.ExecContext(ctx, `
UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=? AND execution_deadline_at_ms>?
`, now+60_000, now, now, candidate.jobID, candidate.workerID, now)); err != nil {
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

func (service *Service) validateMultiDiscAttachmentContents(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	if err := validateMultiDiscAttachmentSet(candidate.expectedMissing, candidate.uploadFiles); err != nil {
		return err
	}
	playlist, files, err := service.multiDiscAttachmentValidationFiles(ctx, candidate)
	if err != nil {
		return err
	}
	playlistBytes, err := service.readMultiDiscAttachmentPlaylist(playlist)
	if err != nil {
		return err
	}
	parsed, err := multidisc.Parse(playlistBytes, files, multidisc.Limits{
		MaxDiscs: candidate.input.MaxDiscs, MaxTotalBytes: candidate.input.MaxTotalBytes,
	})
	if err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, err)
	}
	if !multiDiscEntriesPresent(parsed.Entries) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return multiDiscAttachmentStoreError("write canonical playlist", err)
	}
	candidate.canonicalPlaylist = canonical
	candidate.resultEntries = parsed.Entries
	return service.buildMultiDiscAttachmentManifest(candidate, playlist)
}

func (service *Service) multiDiscAttachmentValidationFiles(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) (attachedMultiDiscFile, []multidisc.File, error) {
	var playlist attachedMultiDiscFile
	files := make([]multidisc.File, 0, len(candidate.baseEntries))
	for _, file := range candidate.baseFiles {
		if file.role == "PLAYLIST_SOURCE" {
			if playlist.blobID != "" {
				return attachedMultiDiscFile{}, nil,
					multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
			}
			playlist = file
			continue
		}
		validated, err := service.multiDiscFileForValidation(ctx, candidate, file)
		if err != nil {
			return attachedMultiDiscFile{}, nil, err
		}
		files = append(files, validated)
	}
	for _, file := range candidate.uploadFiles {
		validated, err := service.multiDiscFileForValidation(ctx, candidate, file)
		if err != nil {
			return attachedMultiDiscFile{}, nil, err
		}
		files = append(files, validated)
	}
	if playlist.blobID == "" || playlist.blobSize > multidisc.MaxPlaylistBytes {
		return attachedMultiDiscFile{}, nil,
			multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return playlist, files, nil
}

func (service *Service) readMultiDiscAttachmentPlaylist(playlist attachedMultiDiscFile) ([]byte, error) {
	reader, err := service.blobs.OpenDigest(playlist.blobSHA)
	if err != nil {
		return nil, multiDiscAttachmentStoreError("open playlist", err)
	}
	playlistBytes, readErr := io.ReadAll(io.LimitReader(reader, multidisc.MaxPlaylistBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(playlistBytes)) != playlist.blobSize {
		return nil, multiDiscAttachmentStoreError("read playlist", errors.Join(readErr, closeErr))
	}
	return playlistBytes, nil
}

func multiDiscEntriesPresent(entries []multidisc.Entry) bool {
	for _, entry := range entries {
		if entry.State != multidisc.EntryPresent {
			return false
		}
	}
	return true
}

func (service *Service) buildMultiDiscAttachmentManifest(
	candidate *multiDiscAttachmentCandidate,
	playlist attachedMultiDiscFile,
) error {
	files := make([]attachedMultiDiscFile, 0, len(candidate.resultEntries)+1)
	playlist.role, playlist.sortOrder = "PLAYLIST_SOURCE", 0
	files = append(files, playlist)
	manifestFiles := make([]contentmanifest.File, 0, len(candidate.resultEntries)+1)
	manifestFiles = append(manifestFiles, contentmanifest.File{
		Role: playlist.role, LogicalName: playlist.logicalName,
		BlobSHA256: playlist.blobSHA, SizeBytes: playlist.blobSize,
	})
	for _, entry := range candidate.resultEntries {
		file := attachedMultiDiscFile{
			role: "DISC", logicalName: entry.File.LogicalName, uploadFileID: entry.File.UploadFileID,
			blobID: entry.File.BlobID, blobSHA: entry.File.BlobSHA256,
			blobSize: entry.File.SizeBytes, sortOrder: entry.Ordinal,
		}
		files = append(files, file)
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.blobSHA, SizeBytes: file.blobSize,
		})
	}
	manifest, digest, err := contentmanifest.Build(manifestFiles)
	if err != nil {
		return multiDiscAttachmentStoreError("build manifest", err)
	}
	candidate.baseFiles = files
	candidate.resultManifestJSON, candidate.resultManifestDigest = string(manifest), digest
	return nil
}

func (service *Service) resolveMultiDiscAttachmentValidation(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
) ([]preparedValidationFile, string, error) {
	if len(candidate.resultEntries) < multidisc.MinDiscs {
		return nil, "", ErrInvalid
	}
	snapshot, status, code, err := corevalidation.ResolveBIOS(
		ctx, transaction, candidate.input.CoreArtifactID, candidate.resultEntries[0].File.LogicalName,
	)
	if err != nil {
		return nil, "", multiDiscAttachmentStoreError("resolve BIOS", err)
	}
	snapshot.MultiDisc = &corevalidation.MultiDiscSnapshot{
		ContentKind:   corevalidation.MultiDiscContentKind,
		ParserVersion: corevalidation.MultiDiscParserVersion,
		DiscCount:     len(candidate.resultEntries), MissingEntries: []corevalidation.MultiDiscMissingEntry{},
		OrderedDiscSHA256:       make([]string, 0, len(candidate.resultEntries)),
		CanonicalPlaylistSHA256: candidate.canonicalPlaylist.SHA256,
		Delivery:                corevalidation.MultiDiscDelivery,
	}
	for _, entry := range candidate.resultEntries {
		snapshot.MultiDisc.OrderedDiscSHA256 = append(
			snapshot.MultiDisc.OrderedDiscSHA256, entry.File.BlobSHA256,
		)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return nil, "", multiDiscAttachmentStoreError("encode dependency snapshot", err)
	}
	candidate.resultDependencySnapshot = snapshot
	candidate.validationStatus, candidate.compatibilityCode = status, code
	files := make([]preparedValidationFile, 0, len(snapshot.BIOS)+1)
	for _, dependency := range snapshot.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			files = append(files, preparedValidationFile{
				role: "BIOS_BUNDLE", logicalName: dependency.LogicalName,
				blobID: *dependency.BlobID, sortOrder: len(files),
			})
		}
	}
	return files, string(encoded), nil
}

func currentMultiDiscAttachmentInput(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
) (string, error) {
	var itemState, snapshotID, platformID, targetID, coreID, artifactID, compatibility string
	var platformVersion, artifactVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT item.state,draft.effective_source_snapshot_id,platform.platform_id,platform.id,
platform.version,platform.default_core_id,artifact.id,artifact.version,artifact.compatibility_config_json
FROM import_items item
JOIN review_drafts draft ON draft.id=? AND draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.enabled=1
WHERE item.id=?
`, candidate.input.ReviewDraftID, candidate.input.ImportItemID).Scan(
		&itemState, &snapshotID, &platformID, &targetID, &platformVersion, &coreID,
		&artifactID, &artifactVersion, &compatibility,
	); err != nil {
		return "", multiDiscAttachmentStoreError("read current input", err)
	}
	if itemState != "REVIEW_PENDING" || snapshotID != candidate.input.BaseSourceSnapshotID ||
		platformID != candidate.input.TargetPlatformID || targetID != candidate.input.PlatformInstanceID ||
		platformVersion != candidate.input.PlatformVersion || coreID != candidate.input.CoreID ||
		artifactID != candidate.input.CoreArtifactID || artifactVersion != candidate.input.CoreArtifactVersion ||
		compatibilityConfigDigest(compatibility) != candidate.input.CompatibilityDigest {
		return "", ErrInvalid
	}
	capabilities := contentcapability.Resolve(platformID, true, true, compatibility)
	if capabilities.MultiDisc == nil || capabilities.MultiDisc.MaxDiscs != candidate.input.MaxDiscs ||
		capabilities.MultiDisc.MaxTotalBytes != candidate.input.MaxTotalBytes {
		return "", ErrInvalid
	}
	return compatibility, nil
}

func verifyMultiDiscAttachmentOwnership(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
) error {
	var jobState, workerID string
	if err := transaction.QueryRowContext(ctx, `SELECT state,worker_id FROM jobs WHERE id=?`, candidate.jobID).
		Scan(&jobState, &workerID); err != nil || jobState != "RUNNING" || workerID != candidate.workerID {
		return ErrInvalid
	}
	var consumed int
	if err := transaction.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM upload_consumptions
  WHERE upload_session_id=? AND upload_file_id IS NULL
)
`, candidate.input.UploadSessionID).Scan(&consumed); err != nil || consumed != 0 {
		return ErrInvalid
	}
	entries, err := loadMultiDiscEntries(ctx, transaction, candidate.input.BaseSourceSnapshotID)
	if err != nil {
		return err
	}
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil || digest != candidate.input.ExpectedSetDigest {
		return ErrInvalid
	}
	return nil
}

func insertMultiDiscSourceSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
	snapshotID string,
	revision int,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(id,import_item_id,revision_no,content_kind,
source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,?,'MULTI_DISC_M3U_V1',?,?,'MULTI_DISC_ATTACHMENT',?)
`, snapshotID, candidate.input.ImportItemID, revision,
		candidate.resultManifestJSON, candidate.resultManifestDigest, now); err != nil {
		return multiDiscAttachmentStoreError("insert source snapshot", err)
	}
	for _, file := range candidate.baseFiles {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,
upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,?,?,?,?,NULL,NULL,?,?)
`, snapshotID, file.role, file.logicalName, file.uploadFileID, file.blobID, file.sortOrder, now); err != nil {
			return multiDiscAttachmentStoreError("insert source file", err)
		}
	}
	for _, entry := range candidate.resultEntries {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_multidisc_entries(source_snapshot_id,ordinal,source_reference,
normalized_reference,canonical_name,state,upload_file_id,blob_id,source_logical_name,created_at_ms)
VALUES(?,?,?,?,?,'PRESENT',?,?,?,?)
`, snapshotID, entry.Ordinal, entry.SourceReference, entry.NormalizedReference, entry.CanonicalName,
			entry.File.UploadFileID, entry.File.BlobID, entry.File.LogicalName, now); err != nil {
			return multiDiscAttachmentStoreError("insert disc entry", err)
		}
	}
	return nil
}

func insertMultiDiscValidation(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
	snapshotID, validationID, dependencyJSON string,
	files []preparedValidationFile,
	now int64,
) error {
	canonicalBlobID, err := blobstore.EnsureRecord(
		ctx, transaction, candidate.canonicalPlaylist, "application/vnd.retrom.m3u", now,
	)
	if err != nil {
		return multiDiscAttachmentStoreError("register canonical playlist", err)
	}
	inputDigest := prepublishDigest(prepublishDigestInput{
		SchemaVersion: 1, ValidatorVersion: validatorMultiV4,
		SourceSnapshotID: snapshotID, SourceManifestDigest: candidate.resultManifestDigest,
		ContentKind: multidisc.ContentKind, TargetPlatformInstanceID: candidate.input.PlatformInstanceID,
		PlatformInstanceVersion:   candidate.input.PlatformVersion,
		CoreArtifactID:            candidate.input.CoreArtifactID,
		CoreArtifactVersion:       candidate.input.CoreArtifactVersion,
		CompatibilityConfigDigest: candidate.input.CompatibilityDigest,
		DependencySnapshot:        json.RawMessage(dependencyJSON), Status: candidate.validationStatus,
		CompatibilityCode: candidate.compatibilityCode,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,core_artifact_id,core_artifact_version,prepublish_generation,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,?,?,?,4,NULL,NULL,?,?,?,?,?,?,?)
`, validationID, candidate.input.ImportItemID, candidate.input.PlatformInstanceID,
		candidate.input.PlatformVersion, candidate.input.CoreID, candidate.input.CoreArtifactID,
		candidate.input.CoreArtifactVersion, candidate.resultManifestDigest, snapshotID, inputDigest,
		candidate.validationStatus, candidate.compatibilityCode, dependencyJSON, now); err != nil {
		return multiDiscAttachmentStoreError("insert validation", err)
	}
	files = append(files, preparedValidationFile{
		role: "MULTI_DISC_PLAYLIST", logicalName: "playlist.m3u", blobID: canonicalBlobID, sortOrder: 0,
	})
	for _, file := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
sort_order,created_at_ms) VALUES(?,?,?,?,?,?)
`, validationID, file.role, file.logicalName, file.blobID, file.sortOrder, now); err != nil {
			return multiDiscAttachmentStoreError("insert validation file", err)
		}
	}
	return nil
}

func recordMultiDiscDuplicateEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, platformID string,
	now int64,
) error {
	identity, err := importItemContentIdentity(ctx, transaction, itemID)
	if err != nil {
		return err
	}
	if err := claimContentIdentity(ctx, transaction, platformID, identity, now); err != nil {
		return err
	}
	duplicates, err := findDuplicateGames(ctx, transaction, itemID, platformID)
	if err != nil {
		return err
	}
	for _, game := range duplicates {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_duplicate_matches(import_item_id,existing_game_id,
existing_game_content_revision_id,content_identity_digest,detected_stage,created_at_ms)
VALUES(?,?,?,?,'IDENTIFICATION',?) ON CONFLICT(import_item_id,existing_game_id) DO NOTHING
`, itemID, game.GameID, game.CurrentContentRevisionID, identity, now); err != nil {
			return multiDiscAttachmentStoreError("insert duplicate evidence", err)
		}
	}
	return nil
}

type acceptedMultiDiscEvidence struct {
	sourceSnapshotID, validationID string
	now                            int64
}

func (service *Service) createAcceptedMultiDiscEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
) (acceptedMultiDiscEvidence, error) {
	validationFiles, dependencyJSON, err := service.resolveMultiDiscAttachmentValidation(ctx, transaction, candidate)
	if err != nil {
		return acceptedMultiDiscEvidence{}, multiDiscAttachmentStoreError("resolve validation", err)
	}
	var baseRevision int
	if err := transaction.QueryRowContext(ctx, `
SELECT revision_no FROM import_item_source_snapshots WHERE id=? AND import_item_id=?
`, candidate.input.BaseSourceSnapshotID, candidate.input.ImportItemID).Scan(&baseRevision); err != nil {
		return acceptedMultiDiscEvidence{}, multiDiscAttachmentStoreError("read base revision", err)
	}
	newSnapshotID, _ := uuid.NewV7()
	validationID, _ := uuid.NewV7()
	evidence := acceptedMultiDiscEvidence{
		sourceSnapshotID: newSnapshotID.String(), validationID: validationID.String(),
		now: service.now().UnixMilli(),
	}
	if err := insertMultiDiscSourceSnapshot(
		ctx, transaction, *candidate, evidence.sourceSnapshotID, baseRevision+1, evidence.now,
	); err != nil {
		return acceptedMultiDiscEvidence{}, err
	}
	if err := insertMultiDiscValidation(
		ctx, transaction, candidate, evidence.sourceSnapshotID, evidence.validationID,
		dependencyJSON, validationFiles, evidence.now,
	); err != nil {
		return acceptedMultiDiscEvidence{}, err
	}
	return evidence, nil
}

func expectOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if changed != 1 {
		return ErrInvalid
	}
	return nil
}

func (service *Service) advanceAcceptedMultiDiscState(
	ctx context.Context,
	transaction *sql.Tx,
	candidate *multiDiscAttachmentCandidate,
	evidence acceptedMultiDiscEvidence,
) error {
	selectedValidation := any(nil)
	if candidate.validationStatus == "READY" {
		selectedValidation = evidence.validationID
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE review_drafts SET effective_source_snapshot_id=?,selected_validation_id=?,
version=version+1,updated_at_ms=? WHERE id=? AND effective_source_snapshot_id=?
`, evidence.sourceSnapshotID, selectedValidation, evidence.now,
		candidate.input.ReviewDraftID, candidate.input.BaseSourceSnapshotID)); err != nil {
		return multiDiscAttachmentStoreError("advance review source", err)
	}
	if err := recordMultiDiscDuplicateEvidence(
		ctx, transaction, candidate.input.ImportItemID, candidate.input.TargetPlatformID, evidence.now,
	); err != nil {
		return err
	}
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "discCount": len(candidate.resultEntries),
		"attachedFileCount": len(candidate.uploadFiles), "validationStatus": candidate.validationStatus,
	})
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='ACCEPTED',result_source_snapshot_id=?,
result_validation_id=?,diagnostics_json=?,error_code=NULL,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, evidence.sourceSnapshotID, evidence.validationID, string(diagnostics), evidence.now, evidence.now,
		candidate.input.AttachmentID)); err != nil {
		return multiDiscAttachmentStoreError("accept attachment", err)
	}
	consumptionID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,NULL,'REVIEW_MULTI_DISC',?,?)
`, consumptionID.String(), candidate.input.UploadSessionID, candidate.input.AttachmentID, evidence.now); err != nil {
		return multiDiscAttachmentStoreError("consume upload", err)
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE import_items SET version=version+1,updated_at_ms=?
WHERE id=? AND state='REVIEW_PENDING'
`, evidence.now, candidate.input.ImportItemID)); err != nil {
		return multiDiscAttachmentStoreError("advance import item", err)
	}
	return nil
}

func recordAcceptedMultiDiscReviewEvent(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
	evidence acceptedMultiDiscEvidence,
) error {
	eventID, _ := uuid.NewV7()
	eventEvidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": candidate.input.AttachmentID,
		"baseSourceSnapshotId":   candidate.input.BaseSourceSnapshotID,
		"resultSourceSnapshotId": evidence.sourceSnapshotID, "validationId": evidence.validationID,
		"validationStatus": candidate.validationStatus, "state": "ACCEPTED",
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_ATTACHMENT_ACCEPTED','USER',?,NULL,'{}',?,?,'{}','{}','{}',?)
`, eventID.String(), candidate.input.ImportItemID, candidate.input.RequestedByUserID,
		string(eventEvidence), string(eventEvidence), evidence.now); err != nil {
		return multiDiscAttachmentStoreError("record review event", err)
	}
	return nil
}

func completeAcceptedMultiDiscJob(
	ctx context.Context,
	transaction *sql.Tx,
	candidate multiDiscAttachmentCandidate,
	evidence acceptedMultiDiscEvidence,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'PLAYLIST_PARSED','{}',?),
(?,'IMPORT_ITEM',?,'DISC_SET_MATCHED','{}',?),
(?,'IMPORT_ITEM',?,'SOURCE_SNAPSHOT_CREATED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?),
(?,'IMPORT_ITEM',?,'SUCCEEDED','{}',?)
`, candidate.jobID, candidate.input.ImportItemID, evidence.now,
		candidate.jobID, candidate.input.ImportItemID, evidence.now,
		candidate.jobID, candidate.input.ImportItemID,
		fmt.Sprintf(`{"sourceSnapshotId":%q}`, evidence.sourceSnapshotID), evidence.now,
		candidate.jobID, candidate.input.ImportItemID,
		fmt.Sprintf(`{"validationId":%q,"status":%q}`, evidence.validationID, candidate.validationStatus), evidence.now,
		candidate.jobID, candidate.input.ImportItemID, evidence.now); err != nil {
		return multiDiscAttachmentStoreError("record job events", err)
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, evidence.now, evidence.now, candidate.jobID, candidate.workerID)); err != nil {
		return multiDiscAttachmentStoreError("complete job", err)
	}
	return nil
}

func (service *Service) commitAcceptedMultiDiscAttachment(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return multiDiscAttachmentStoreError("begin accepted commit", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := currentMultiDiscAttachmentInput(ctx, transaction, *candidate); err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	if err := verifyMultiDiscAttachmentOwnership(ctx, transaction, *candidate); err != nil {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	evidence, err := service.createAcceptedMultiDiscEvidence(ctx, transaction, candidate)
	if err != nil {
		return err
	}
	if err := service.advanceAcceptedMultiDiscState(ctx, transaction, candidate, evidence); err != nil {
		return err
	}
	if err := recordAcceptedMultiDiscReviewEvent(ctx, transaction, *candidate, evidence); err != nil {
		return err
	}
	if err := completeAcceptedMultiDiscJob(ctx, transaction, *candidate, evidence); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return multiDiscAttachmentStoreError("commit accepted attachment", err)
	}
	return nil
}

func (service *Service) runMultiDiscAttachment(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, multiDiscAttachmentDeadline)
	defer cancel()
	candidate, err := service.claimMultiDiscAttachment(ctx, jobID)
	if err != nil {
		return
	}
	if err := service.readAttachedMultiDiscBase(ctx, &candidate); err != nil {
		service.finishRejectedMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorInputStale, err)
		return
	}
	if err := service.readMultiDiscAttachmentUploads(ctx, &candidate); err != nil {
		service.finishRejectedMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorInputStale, err)
		return
	}
	if err := service.validateMultiDiscAttachmentContents(ctx, &candidate); err != nil {
		if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
			return
		}
		code := MultiDiscAttachmentErrorCode(err)
		if code == "" {
			service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, err)
			return
		}
		service.finishRejectedMultiDiscAttachment(ctx, candidate, code, err)
		return
	}
	if err := service.commitAcceptedMultiDiscAttachment(ctx, &candidate); err != nil {
		if service.finishMultiDiscAttachmentCancellation(ctx, candidate) {
			return
		}
		code := MultiDiscAttachmentErrorCode(err)
		if code == MultiDiscAttachmentErrorInputStale {
			service.finishRejectedMultiDiscAttachment(ctx, candidate, code, err)
			return
		}
		service.finishRetryableMultiDiscAttachment(ctx, candidate, MultiDiscAttachmentErrorUnavailable, err)
	}
}

func (service *Service) finishRejectedMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	cause error,
) {
	now := service.now().UnixMilli()
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "errorCode": code, "causeCode": MultiDiscAttachmentErrorCode(cause),
	})
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='REJECTED',error_code=?,diagnostics_json=?,
finished_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, code, string(diagnostics), now, now, candidate.input.AttachmentID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, candidate.jobID, candidate.workerID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'DISC_SET_REJECTED',?,?),
(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, string(diagnostics), now,
		candidate.jobID, candidate.input.ImportItemID, string(diagnostics), now); err != nil {
		return
	}
	eventID, _ := uuid.NewV7()
	evidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": candidate.input.AttachmentID,
		"baseSourceSnapshotId": candidate.input.BaseSourceSnapshotID,
		"state":                "REJECTED", "errorCode": code,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DISC_ATTACHMENT_REJECTED','USER',?,NULL,'{}',?,?,'{}','{}','{}',?)
`, eventID.String(), candidate.input.ImportItemID, candidate.input.RequestedByUserID,
		string(evidence), string(evidence), now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) finishRetryableMultiDiscAttachment(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
	_ error,
) {
	if service.scheduleMultiDiscAttachmentRetry(ctx, candidate, code) {
		return
	}
	now := service.now().UnixMilli()
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='FAILED_RETRYABLE',error_code=?,diagnostics_json=?,
finished_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, code, diagnostics, now, now, candidate.input.AttachmentID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,
leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, candidate.jobID, candidate.workerID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, diagnostics, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func multiDiscAttachmentRetryDelay(attempt int64) time.Duration {
	delay := 250 * time.Millisecond
	for current := int64(1); current < attempt && delay < 4*time.Second; current++ {
		delay *= 2
	}
	return delay
}

func (service *Service) scheduleMultiDiscAttachmentRetry(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
	code string,
) bool {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	var attemptCount, maxAttempts, deadline int64
	if err := transaction.QueryRowContext(ctx, `
SELECT attempt_count,max_attempts,execution_deadline_at_ms
FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?
`, candidate.jobID, candidate.workerID).Scan(&attemptCount, &maxAttempts, &deadline); err != nil {
		return false
	}
	now := service.now().UnixMilli()
	delay := multiDiscAttachmentRetryDelay(attemptCount)
	availableAt := now + delay.Milliseconds()
	if attemptCount >= maxAttempts || availableAt >= deadline {
		return false
	}
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='FAILED_RETRYABLE',error_code=?,diagnostics_json=?,
finished_at_ms=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, code, diagnostics, now, now, candidate.input.AttachmentID)); err != nil {
		return false
	}
	if err := expectOneRow(transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,error_code=NULL,error_retryable=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, availableAt, now, candidate.jobID, candidate.workerID)); err != nil {
		return false
	}
	event := fmt.Sprintf(`{"attempt":%d,"retryAtMs":%d}`, attemptCount, availableAt)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'RETRY_SCHEDULED',?,?)
`, candidate.jobID, candidate.input.ImportItemID, event, now); err != nil {
		return false
	}
	if err := transaction.Commit(); err != nil {
		return false
	}
	service.scheduleMultiDiscAttachmentRun(ctx, candidate.jobID, delay)
	return true
}

func (service *Service) SyncMultiDiscAttachmentCancellation(ctx context.Context, jobID string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE')
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='CANCELLED')
`, now, now, jobID, jobID)
}

func (service *Service) finishMultiDiscAttachmentCancellation(
	ctx context.Context,
	candidate multiDiscAttachmentCandidate,
) bool {
	var state string
	if err := service.database.QueryRowContext(
		ctx, `SELECT state FROM jobs WHERE id=? AND worker_id=?`, candidate.jobID, candidate.workerID,
	).Scan(&state); err != nil || state != "CANCEL_REQUESTED" {
		return false
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, now, candidate.input.AttachmentID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, now, now, candidate.jobID, candidate.workerID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED','{}',?)
`, candidate.jobID, candidate.input.ImportItemID, now); err != nil {
		return false
	}
	return transaction.Commit() == nil
}

func (service *Service) ReviewMultiDisc(
	ctx context.Context,
	itemID string,
) (any, bool, error) {
	var snapshotID, contentKind string
	if err := service.database.QueryRowContext(ctx, `
SELECT snapshot.id,snapshot.content_kind
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&snapshotID, &contentKind); err != nil {
		return nil, false, multiDiscAttachmentStoreError("read review", err)
	}
	if contentKind != multidisc.ContentKind {
		return nil, false, nil
	}
	projection, missingCount, err := service.reviewMultiDiscSourceProjection(ctx, snapshotID)
	if err != nil {
		return nil, false, err
	}
	attachments, active, retryRequired, err := service.reviewMultiDiscAttachments(ctx, itemID)
	if err != nil {
		return nil, false, err
	}
	latest := any(nil)
	if len(attachments) > 0 {
		latest = attachments[0]
	}
	projection["latestAttachment"] = latest
	projection["activeAttachment"] = active
	projection["canAttachMissingDiscs"] = missingCount > 0 && active == nil && !retryRequired
	return projection, true, nil
}

func (service *Service) reviewMultiDiscSourceProjection(
	ctx context.Context,
	snapshotID string,
) (map[string]any, int, error) {
	var playlistName, playlistSHA string
	var playlistSize, maxTotalBytes int64
	var maxDiscs int
	if err := service.database.QueryRowContext(ctx, `
SELECT file.logical_name,blob.size_bytes,blob.sha256,
coalesce(json_extract(job.config_snapshot_json,'$.multiDisc.maxDiscs'),?),
coalesce(json_extract(job.config_snapshot_json,'$.multiDisc.maxTotalBytes'),?)
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
JOIN import_items item ON item.id=snapshot.import_item_id
JOIN import_jobs job ON job.id=item.import_job_id
WHERE file.source_snapshot_id=? AND file.role='PLAYLIST_SOURCE'
	`, multidisc.MaxDiscs, multidisc.MaxTotalBytes, snapshotID).Scan(
		&playlistName, &playlistSize, &playlistSHA, &maxDiscs, &maxTotalBytes,
	); err != nil {
		return nil, 0, multiDiscAttachmentStoreError("read review playlist", err)
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT entry.ordinal,entry.source_reference,entry.canonical_name,entry.state,
entry.source_logical_name,blob.size_bytes,blob.sha256
FROM import_item_multidisc_entries entry
LEFT JOIN blobs blob ON blob.id=entry.blob_id
WHERE entry.source_snapshot_id=? ORDER BY entry.ordinal
	`, snapshotID)
	if err != nil {
		return nil, 0, multiDiscAttachmentStoreError("read review entries", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0, multidisc.MaxDiscs)
	missing := make([]string, 0)
	var totalSize int64
	presentCount := 0
	for rows.Next() {
		var ordinal int
		var reference, canonicalName, state string
		var logicalName, blobSHA sql.NullString
		var size sql.NullInt64
		if err := rows.Scan(
			&ordinal, &reference, &canonicalName, &state, &logicalName, &size, &blobSHA,
		); err != nil {
			return nil, 0, multiDiscAttachmentStoreError("scan review entries", err)
		}
		if size.Valid {
			totalSize += size.Int64
			presentCount++
		}
		if state == "MISSING" {
			missing = append(missing, reference)
		}
		entries = append(entries, map[string]any{
			"index": ordinal, "discIndex": ordinal, "label": fmt.Sprintf("光盘 %d", ordinal+1),
			"sourceReference": reference, "canonicalName": canonicalName, "state": state,
			"logicalName": nullable(logicalName), "sizeBytes": nullableInt(size), "sha256": nullable(blobSHA),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, multiDiscAttachmentStoreError("iterate review entries", err)
	}
	return map[string]any{
		"contentKind": multidisc.ContentKind,
		"playlist": map[string]any{
			"name": playlistName, "sizeBytes": playlistSize, "sha256": playlistSHA,
		},
		"discCount": len(entries), "presentDiscCount": presentCount, "missingDiscCount": len(missing),
		"totalPresentBytes": totalSize, "maxDiscs": maxDiscs, "maxTotalBytes": maxTotalBytes,
		"entries": entries, "missingReferences": missing,
	}, len(missing), nil
}

func (service *Service) reviewMultiDiscAttachments(
	ctx context.Context,
	itemID string,
) ([]map[string]any, any, bool, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT attachment.id,attachment.state,attachment.error_code,attachment.diagnostics_json,
attachment.job_id,job.state,job.error_retryable,job.version,
attachment.version,attachment.created_at_ms,attachment.updated_at_ms,attachment.finished_at_ms
FROM review_multidisc_attachments attachment
JOIN jobs job ON job.id=attachment.job_id
WHERE attachment.import_item_id=? ORDER BY attachment.created_at_ms DESC,attachment.id DESC
`, itemID)
	if err != nil {
		return nil, nil, false, multiDiscAttachmentStoreError("read review attachments", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	attachments := make([]map[string]any, 0)
	var active any
	retryRequired := false
	for rows.Next() {
		var id, state, diagnosticsJSON, jobID, jobState string
		var errorCode sql.NullString
		var retryable sql.NullInt64
		var jobVersion, attachmentVersion, createdAt, updatedAt int64
		var finishedAt sql.NullInt64
		if err := rows.Scan(
			&id, &state, &errorCode, &diagnosticsJSON, &jobID, &jobState, &retryable,
			&jobVersion, &attachmentVersion, &createdAt, &updatedAt, &finishedAt,
		); err != nil {
			return nil, nil, false, multiDiscAttachmentStoreError("scan review attachments", err)
		}
		var diagnostics any
		_ = json.Unmarshal([]byte(diagnosticsJSON), &diagnostics)
		value := map[string]any{
			"attachmentId": id, "state": state, "errorCode": nullable(errorCode),
			"diagnostics": diagnostics, "jobId": jobID, "jobState": jobState,
			"version": attachmentVersion, "jobVersion": jobVersion,
			"canRetry": state == "FAILED_RETRYABLE" &&
				jobState == "FAILED" && retryable.Valid && retryable.Int64 == 1,
			"createdAtMs": createdAt, "updatedAtMs": updatedAt, "finishedAtMs": nullableInt(finishedAt),
		}
		attachments = append(attachments, value)
		activeState := state == "QUEUED" || state == "RUNNING" || state == "FAILED_RETRYABLE"
		if active == nil && activeState && (jobState == "QUEUED" || jobState == "RUNNING") {
			active = value
		}
		if state == "FAILED_RETRYABLE" {
			retryRequired = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, multiDiscAttachmentStoreError("iterate review attachments", err)
	}
	return attachments, active, retryRequired, nil
}
