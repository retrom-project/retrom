package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/contentmanifest"
	"retrom/internal/importing"
)

const parentAttachmentDeadline = 30 * time.Minute

const (
	ParentErrorInvalid       = "REVIEW_PARENT_UPLOAD_INVALID"
	ParentErrorNotFound      = "REVIEW_NOT_FOUND"
	ParentErrorVersion       = "REVIEW_VERSION_CONFLICT"
	ParentErrorInProgress    = "REVIEW_PARENT_ATTACHMENT_IN_PROGRESS"
	ParentErrorInputStale    = "REVIEW_PARENT_INPUT_STALE"
	ParentErrorFinalized     = "REVIEW_ALREADY_FINALIZED"
	ParentErrorNotRequired   = "REVIEW_PARENT_NOT_REQUIRED"
	ParentErrorArchiveUnsafe = "REVIEW_PARENT_ARCHIVE_UNSAFE"
	ParentErrorMismatch      = "REVIEW_PARENT_CONTENT_MISMATCH"
	ParentErrorStructure     = "REVIEW_PARENT_STRUCTURE_UNSUPPORTED"
	ParentErrorUnavailable   = "REVIEW_PARENT_VALIDATION_UNAVAILABLE"
)

type ParentAttachmentError struct {
	Code  string
	Cause error
}

func (value *ParentAttachmentError) Error() string {
	if value.Cause == nil {
		return value.Code
	}
	return fmt.Sprintf("%s: %v", value.Code, value.Cause)
}
func (value *ParentAttachmentError) Unwrap() error { return value.Cause }

func parentError(code string, cause error) error {
	return &ParentAttachmentError{Code: code, Cause: cause}
}

func parentStoreError(operation string, err error) error {
	return fmt.Errorf("libraryimport/arcade parent %s: %w", operation, err)
}

func ParentAttachmentErrorCode(err error) string {
	var value *ParentAttachmentError
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

type ParentAttachmentRequest struct {
	ValidationID         string `json:"validationId"`
	BaseSourceSnapshotID string `json:"baseSourceSnapshotId"`
	DependencyMachine    string `json:"dependencyMachine"`
	UploadFileID         string `json:"uploadFileId"`
}

type ParentAttachmentCreated struct {
	AttachmentID string `json:"attachmentId"`
	State        string `json:"state"`
	JobID        string `json:"jobId"`
	Version      int64  `json:"-"`
}

type parentAttachmentInput struct {
	SchemaVersion        int    `json:"schemaVersion"`
	AttachmentID         string `json:"attachmentId"`
	ImportItemID         string `json:"importItemId"`
	ReviewDraftID        string `json:"reviewDraftId"`
	BaseSourceSnapshotID string `json:"baseSourceSnapshotId"`
	DependencyMachine    string `json:"dependencyMachine"`
	CoreArtifactID       string `json:"coreArtifactId"`
	DATVersionID         string `json:"datVersionId"`
	UploadFileID         string `json:"uploadFileId"`
}

type parentAttachmentCandidate struct {
	attachmentID, itemID, draftID, baseSnapshotID string
	machine, requiredBy, artifactID, datID        string
	uploadFileID, uploadSessionID, originalName   string
	blobID, blobSHA                               string
	blobSize                                      int64
	depth                                         int
}

//nolint:funlen,gocognit,gocyclo // Preconditions intentionally share one transaction and one stable error mapping.
func (service *Service) CreateArcadeParentAttachment(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	request ParentAttachmentRequest,
) (ParentAttachmentCreated, error) {
	if expectedVersion < 1 || itemID == "" || request.ValidationID == "" || request.BaseSourceSnapshotID == "" ||
		request.UploadFileID == "" || !validArcadeMachine(request.DependencyMachine) || service.blobs == nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorInvalid, ErrInvalid)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	defer cleanup.Rollback(transaction)
	var draftID, itemState, targetID, effectiveSnapshotID, platformID, coreID, artifactID string
	var activeDATID sql.NullString
	var draftVersion, platformVersion int64
	err = transaction.QueryRowContext(ctx, `
SELECT draft.id,item.state,draft.version,draft.target_platform_instance_id,
draft.effective_source_snapshot_id,platform.platform_id,platform.version,
platform.default_core_id,artifact.id,
(SELECT dat.id FROM dat_versions dat WHERE dat.core_artifact_id=artifact.id AND dat.is_active=1)
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.enabled=1
WHERE item.id=?
`, itemID).Scan(
		&draftID, &itemState, &draftVersion, &targetID, &effectiveSnapshotID, &platformID,
		&platformVersion, &coreID, &artifactID, &activeDATID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ParentAttachmentCreated{}, parentError(ParentErrorNotFound, err)
	}
	if err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if itemState != "REVIEW_PENDING" {
		return ParentAttachmentCreated{}, parentError(ParentErrorFinalized, ErrInvalid)
	}
	if draftVersion != expectedVersion {
		return ParentAttachmentCreated{}, parentError(ParentErrorVersion, ErrInvalid)
	}
	if platformID != "arcade" || effectiveSnapshotID != request.BaseSourceSnapshotID || !activeDATID.Valid {
		return ParentAttachmentCreated{}, parentError(ParentErrorInputStale, ErrInvalid)
	}
	var validationTargetID, validationSnapshotID, validationCoreID, validationArtifactID string
	var validationDATID sql.NullString
	var validationPlatformVersion int64
	var dependencyJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT target_platform_instance_id,platform_instance_version,core_id,core_artifact_id,
dat_version_id,source_snapshot_id,dependency_snapshot_json
FROM import_item_core_validations
WHERE id=? AND import_item_id=?
`, request.ValidationID, itemID).Scan(
		&validationTargetID, &validationPlatformVersion, &validationCoreID, &validationArtifactID,
		&validationDATID, &validationSnapshotID, &dependencyJSON,
	); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorInputStale, err)
	}
	if validationTargetID != targetID || validationPlatformVersion != platformVersion || validationCoreID != coreID ||
		validationArtifactID != artifactID || validationSnapshotID != effectiveSnapshotID ||
		!validationDATID.Valid || validationDATID.String != activeDATID.String {
		return ParentAttachmentCreated{}, parentError(ParentErrorInputStale, ErrInvalid)
	}
	snapshot, err := service.projectArcadeSnapshotV2WithQueryer(ctx, transaction, dependencyJSON)
	if err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorInputStale, err)
	}
	dependency, found := attachmentDependency(snapshot, request.DependencyMachine)
	if !found || dependency.Kind != "PARENT" || dependency.State != "MISSING" && dependency.State != "MISMATCH" {
		return ParentAttachmentCreated{}, parentError(ParentErrorNotRequired, ErrInvalid)
	}
	if dependency.RequiredBy == nil || dependency.Depth < 1 || dependency.Depth > 63 {
		return ParentAttachmentCreated{}, parentError(ParentErrorStructure, ErrInvalid)
	}
	var uploadSessionID, uploadState, fileState, originalName, blobID, blobSHA string
	var blobSize, wholeSessionConsumed int64
	if err := transaction.QueryRowContext(ctx, `
SELECT session.id,session.state,file.state,file.relative_path,file.final_blob_id,
blob.sha256,blob.size_bytes,
EXISTS(SELECT 1 FROM upload_consumptions consumption
WHERE consumption.upload_session_id=session.id AND consumption.upload_file_id IS NULL)
FROM upload_files file
JOIN upload_sessions session ON session.id=file.upload_session_id
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.id=?
`, request.UploadFileID).Scan(
		&uploadSessionID, &uploadState, &fileState, &originalName, &blobID, &blobSHA, &blobSize,
		&wholeSessionConsumed,
	); err != nil || uploadState != "COMPLETE" || fileState != "COMPLETE" || wholeSessionConsumed != 0 ||
		!strings.EqualFold(filepath.Ext(originalName), ".zip") {
		return ParentAttachmentCreated{}, parentError(ParentErrorInvalid, err)
	}
	if info, statErr := os.Stat(service.blobs.Path(blobSHA)); statErr != nil ||
		!info.Mode().IsRegular() || info.Size() != blobSize {
		return ParentAttachmentCreated{}, parentError(ParentErrorInvalid, statErr)
	}
	var activeCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM review_arcade_parent_attachments
WHERE import_item_id=? AND state IN ('QUEUED','RUNNING')
`, itemID).Scan(&activeCount); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if activeCount != 0 {
		return ParentAttachmentCreated{}, parentError(ParentErrorInProgress, ErrInvalid)
	}
	attachmentID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	input := parentAttachmentInput{
		SchemaVersion: 1, AttachmentID: attachmentID.String(), ImportItemID: itemID, ReviewDraftID: draftID,
		BaseSourceSnapshotID: effectiveSnapshotID, DependencyMachine: dependency.Machine,
		CoreArtifactID: artifactID, DATVersionID: activeDATID.String, UploadFileID: request.UploadFileID,
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	now := service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte(strings.Join([]string{
		itemID, effectiveSnapshotID, dependency.Machine, blobSHA, request.ValidationID,
	}, "\x00")))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'IMPORT_ITEM',?,'REVIEW_ARCADE_PARENT_VALIDATE',?,1,?,1,'QUEUED',0,4,?,?,?)
`, jobID.String(), itemID, hex.EncodeToString(dedupe[:]), string(inputJSON), now, now, now); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_arcade_parent_attachments(id,import_item_id,review_draft_id,
base_source_snapshot_id,dependency_machine,expected_logical_name,required_by_machine,depth,
core_artifact_id,dat_version_id,upload_file_id,original_filename,state,diagnostics_json,job_id,
version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'QUEUED','{}',?,1,?,?)
`, attachmentID.String(), itemID, draftID, effectiveSnapshotID, dependency.Machine,
		dependency.Machine+".zip", *dependency.RequiredBy, dependency.Depth, artifactID, activeDATID.String,
		request.UploadFileID, filepath.Base(originalName), jobID.String(), now, now); err != nil {
		if strings.Contains(err.Error(), "review_arcade_parent_active") {
			return ParentAttachmentCreated{}, parentError(ParentErrorInProgress, err)
		}
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{}',?)
`, jobID.String(), itemID, now); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE review_drafts SET version=version+1,updated_at_ms=?
WHERE id=? AND version=?
`, now, draftID, expectedVersion)
	if err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ParentAttachmentCreated{}, parentError(ParentErrorVersion, ErrInvalid)
	}
	eventID, _ := uuid.NewV7()
	evidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": attachmentID.String(), "machine": dependency.Machine,
		"originalFilename": filepath.Base(originalName), "baseSourceSnapshotId": effectiveSnapshotID,
		"validationId": request.ValidationID, "state": "QUEUED",
	})
	actor := reviewActor(ctx)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'PARENT_UPLOAD_REQUESTED',?,?,?,'{}',?,?,'{}',?,'{}',?)
`,
		eventID.String(), itemID, actor.Kind, actor.UserID, actor.Label,
		string(evidence), string(evidence), string(evidence), now,
	); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	if err := transaction.Commit(); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	go service.runParentAttachment(context.WithoutCancel(ctx), jobID.String())
	return ParentAttachmentCreated{
		AttachmentID: attachmentID.String(), State: "QUEUED", JobID: jobID.String(), Version: expectedVersion + 1,
	}, nil
}

func validArcadeMachine(value string) bool {
	return validField(value, 255, false) && value != "" && !strings.ContainsAny(value, "/\\")
}

func attachmentDependency(snapshot arcadeDraftSnapshot, machine string) (arcadeDraftDependency, bool) {
	for _, dependency := range snapshot.Dependencies {
		if dependency.Machine == machine {
			return dependency, true
		}
	}
	return arcadeDraftDependency{}, false
}

func (service *Service) projectArcadeSnapshotV2(
	ctx context.Context,
	raw string,
) (arcadeDraftSnapshot, error) {
	return service.projectArcadeSnapshotV2WithQueryer(ctx, service.database, raw)
}

func (service *Service) projectArcadeSnapshotV2WithQueryer(
	ctx context.Context,
	queryer arcadeRelationQueryer,
	raw string,
) (arcadeDraftSnapshot, error) {
	snapshot, valid := parseArcadeDraftSnapshot(raw)
	if !valid {
		return arcadeDraftSnapshot{}, ErrInvalid
	}
	nodes, cyclic, err := loadArcadeDependencyClosure(ctx, queryer, snapshot.DatVersionID, snapshot.Machine)
	if err != nil || cyclic {
		return arcadeDraftSnapshot{}, ErrInvalid
	}
	byMachine := make(map[string]arcadeClosureNode, len(nodes))
	for _, node := range nodes {
		byMachine[node.Machine] = node
	}
	for index := range snapshot.Dependencies {
		dependency := &snapshot.Dependencies[index]
		node, exists := byMachine[dependency.Machine]
		if !exists || node.Kind != dependency.Kind {
			return arcadeDraftSnapshot{}, ErrInvalid
		}
		dependency.RequiredBy = node.RequiredBy
		dependency.Depth = node.Depth
		dependency.ExpectedLogicalName = dependency.Machine + ".zip"
		dependency.RequiredEntryCount = len(dependency.RequiredEntries)
	}
	closure, err := json.Marshal(nodes)
	if err != nil {
		return arcadeDraftSnapshot{}, fmt.Errorf("project arcade snapshot: %w", err)
	}
	snapshot.SchemaVersion = 2
	snapshot.Closure = closure
	sort.Slice(snapshot.Dependencies, func(left, right int) bool {
		if snapshot.Dependencies[left].Kind != snapshot.Dependencies[right].Kind {
			return snapshot.Dependencies[left].Kind < snapshot.Dependencies[right].Kind
		}
		if snapshot.Dependencies[left].Depth != snapshot.Dependencies[right].Depth {
			return snapshot.Dependencies[left].Depth < snapshot.Dependencies[right].Depth
		}
		return snapshot.Dependencies[left].Machine < snapshot.Dependencies[right].Machine
	})
	return snapshot, nil
}

func (service *Service) ResumeParentAttachmentJobs(ctx context.Context) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id FROM jobs
WHERE kind='REVIEW_ARCADE_PARENT_VALIDATE' AND state='QUEUED'
ORDER BY available_at_ms,id
`)
	if err != nil {
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	jobIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			jobIDs = append(jobIDs, id)
		}
	}
	if rows.Err() != nil {
		return
	}
	workerContext := context.WithoutCancel(ctx)
	for _, jobID := range jobIDs {
		go service.runParentAttachment(workerContext, jobID)
	}
}

//nolint:funlen,gocyclo // Claim, scan, exact DAT match, revalidation, and atomic commit form one worker contract.
func (service *Service) runParentAttachment(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, parentAttachmentDeadline)
	defer cancel()
	candidate, workerID, err := service.claimParentAttachment(ctx, jobID)
	if err != nil {
		return
	}
	entries, err := importing.ScanFlatZIP(ctx, service.blobs.Path(candidate.blobSHA), importing.DefaultArchiveLimits())
	if err != nil {
		code := ParentErrorArchiveUnsafe
		if errors.Is(err, importing.ErrNestedArchiveUnsupported) {
			code = ParentErrorStructure
		}
		service.finishRejectedParentAttachment(ctx, candidate, jobID, workerID, code, archiveReason(err), nil, nil)
		return
	}
	entryByName := make(map[string]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		entryByName[entry.NormalizedPath] = entry
	}
	requirements, hasDisk, err := service.arcadeRequirements(ctx, candidate.datID, candidate.machine)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return
	}
	if hasDisk {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID, ParentErrorStructure, "UNSUPPORTED_CHD", nil, nil,
		)
		return
	}
	missing, mismatched, warnings := matchArcadeRequirements(entryByName, requirements)
	if len(missing) != 0 || len(mismatched) != 0 {
		service.finishRejectedParentAttachment(
			ctx, candidate, jobID, workerID, ParentErrorMismatch, "DAT_ENTRY_MISMATCH", missing, mismatched,
		)
		return
	}
	files, manifestJSON, manifestDigest, err := service.buildAttachedSourceSnapshot(ctx, candidate)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return
	}
	preparedFiles := make([]importSourceFile, 0, len(files))
	for _, file := range files {
		preparedFiles = append(preparedFiles, importSourceFile{
			id: file.uploadFileID, path: file.logicalName, blobID: file.blobID, sha256: file.blobSHA,
		})
	}
	_, groups, _ := service.prepareArcadeFiles(ctx, preparedFiles, sql.NullString{String: candidate.datID, Valid: true})
	rootMachine, err := service.parentAttachmentRootMachine(ctx, candidate)
	if err != nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return
	}
	var selectedGroup *preparedGroup
	for index := range groups {
		for _, source := range groups[index].sources {
			if source.role == "CONTENT" && source.logicalName == rootMachine+".zip" {
				selectedGroup = &groups[index]
				break
			}
		}
	}
	if selectedGroup == nil {
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorUnavailable)
		return
	}
	diagnostics := map[string]any{
		"schemaVersion": 1, "requiredEntryCount": len(requirements), "observedEntryCount": len(entries),
		"warnings": warnings,
	}
	if err := service.commitAcceptedParentAttachment(
		ctx, candidate, jobID, workerID, entries, files, manifestJSON, manifestDigest, *selectedGroup, diagnostics,
	); err != nil {
		if service.finishParentAttachmentCancellation(ctx, candidate, jobID, workerID) {
			return
		}
		service.finishRetryableParentAttachment(ctx, candidate, jobID, workerID, ParentErrorInputStale)
	}
}

func (service *Service) parentAttachmentRootMachine(
	ctx context.Context,
	candidate parentAttachmentCandidate,
) (string, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=? AND source_snapshot_id=? AND core_artifact_id=? AND dat_version_id=?
ORDER BY created_at_ms DESC,id DESC LIMIT 1
	`, candidate.itemID, candidate.baseSnapshotID, candidate.artifactID, candidate.datID).Scan(&raw); err != nil {
		return "", parentStoreError("read root validation", err)
	}
	snapshot, valid := parseArcadeDraftSnapshot(raw)
	if !valid {
		return "", ErrInvalid
	}
	return snapshot.Machine, nil
}

func (service *Service) claimParentAttachment(
	ctx context.Context,
	jobID string,
) (parentAttachmentCandidate, string, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("begin claim", err)
	}
	defer cleanup.Rollback(transaction)
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),execution_deadline_at_ms=?,
leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_ARCADE_PARENT_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
`, workerID.String(), now, now+int64(parentAttachmentDeadline/time.Millisecond),
		now+int64(parentAttachmentDeadline/time.Millisecond), now, now, jobID, now)
	if err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("claim job", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return parentAttachmentCandidate{}, "", ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments
SET state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')
	`, now, jobID); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("mark attachment running", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'STARTED','{}',? FROM jobs WHERE id=?
	`, now, jobID); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("record attachment start", err)
	}
	var candidate parentAttachmentCandidate
	if err := transaction.QueryRowContext(ctx, `
SELECT attachment.id,attachment.import_item_id,attachment.review_draft_id,
attachment.base_source_snapshot_id,attachment.dependency_machine,attachment.required_by_machine,
attachment.depth,attachment.core_artifact_id,attachment.dat_version_id,attachment.upload_file_id,
file.upload_session_id,attachment.original_filename,file.final_blob_id,blob.sha256,blob.size_bytes
FROM review_arcade_parent_attachments attachment
JOIN upload_files file ON file.id=attachment.upload_file_id
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE attachment.job_id=? AND attachment.state='RUNNING'
`, jobID).Scan(
		&candidate.attachmentID, &candidate.itemID, &candidate.draftID, &candidate.baseSnapshotID,
		&candidate.machine, &candidate.requiredBy, &candidate.depth, &candidate.artifactID, &candidate.datID,
		&candidate.uploadFileID, &candidate.uploadSessionID, &candidate.originalName, &candidate.blobID,
		&candidate.blobSHA, &candidate.blobSize,
	); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("read claimed attachment", err)
	}
	if err := transaction.Commit(); err != nil {
		return parentAttachmentCandidate{}, "", parentStoreError("commit claim", err)
	}
	return candidate, workerID.String(), nil
}

type attachedSourceFile struct {
	role, logicalName, uploadFileID, blobID, blobSHA string
	blobSize                                         int64
	archiveBlobID                                    sql.NullString
	archiveOrdinal                                   sql.NullInt64
	sortOrder                                        int
}

func (service *Service) buildAttachedSourceSnapshot(
	ctx context.Context,
	candidate parentAttachmentCandidate,
) ([]attachedSourceFile, string, string, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.blob_id,blob.sha256,blob.size_bytes,
file.source_archive_blob_id,file.source_archive_entry_ordinal
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.source_snapshot_id=?
ORDER BY file.role,file.logical_name
	`, candidate.baseSnapshotID)
	if err != nil {
		return nil, "", "", parentStoreError("read source snapshot", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]attachedSourceFile, 0)
	logicalName := candidate.machine + ".zip"
	replaced := false
	for rows.Next() {
		var file attachedSourceFile
		if err := rows.Scan(
			&file.role, &file.logicalName, &file.uploadFileID, &file.blobID, &file.blobSHA, &file.blobSize,
			&file.archiveBlobID, &file.archiveOrdinal,
		); err != nil {
			return nil, "", "", parentStoreError("scan source snapshot", err)
		}
		if importing.ASCIICaseFold(file.logicalName) == importing.ASCIICaseFold(logicalName) {
			if file.role != "COMPANION" {
				return nil, "", "", ErrInvalid
			}
			file = attachedSourceFile{
				role: "COMPANION", logicalName: logicalName, uploadFileID: candidate.uploadFileID,
				blobID: candidate.blobID, blobSHA: candidate.blobSHA, blobSize: candidate.blobSize,
			}
			replaced = true
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, "", "", parentStoreError("iterate source snapshot", err)
	}
	if !replaced {
		files = append(files, attachedSourceFile{
			role: "COMPANION", logicalName: logicalName, uploadFileID: candidate.uploadFileID,
			blobID: candidate.blobID, blobSHA: candidate.blobSHA, blobSize: candidate.blobSize,
		})
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].role != files[right].role {
			return files[left].role < files[right].role
		}
		return files[left].logicalName < files[right].logicalName
	})
	manifestFiles := make([]contentmanifest.File, 0, len(files))
	for index := range files {
		file := &files[index]
		file.sortOrder = index
		manifest := contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.blobSHA, SizeBytes: file.blobSize,
		}
		if file.archiveBlobID.Valid {
			var archiveSHA string
			if err := service.database.QueryRowContext(ctx, `SELECT sha256 FROM blobs WHERE id=?`, file.archiveBlobID.String).
				Scan(&archiveSHA); err != nil {
				return nil, "", "", parentStoreError("read source archive", err)
			}
			ordinal := int(file.archiveOrdinal.Int64)
			manifest.SourceArchiveSHA256 = &archiveSHA
			manifest.SourceArchiveEntryOrdinal = &ordinal
		}
		manifestFiles = append(manifestFiles, manifest)
	}
	contents, digest, err := contentmanifest.Build(manifestFiles)
	return files, string(contents), digest, err
}

//nolint:funlen,gocognit,gocyclo // Every accepted artifact and audit row must commit atomically.
func (service *Service) commitAcceptedParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
	entries []importing.ArchiveEntry,
	files []attachedSourceFile,
	manifestJSON, manifestDigest string,
	validation preparedGroup,
	diagnostics map[string]any,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return parentStoreError("begin accepted commit", err)
	}
	defer cleanup.Rollback(transaction)
	var itemState, currentSnapshotID, targetID, coreID, artifactID string
	var activeDATID sql.NullString
	var platformVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT item.state,draft.effective_source_snapshot_id,draft.target_platform_instance_id,
platform.version,platform.default_core_id,artifact.id,
(SELECT dat.id FROM dat_versions dat WHERE dat.core_artifact_id=artifact.id AND dat.is_active=1)
FROM import_items item
JOIN review_drafts draft ON draft.id=? AND draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.enabled=1
WHERE item.id=?
`, candidate.draftID, candidate.itemID).Scan(
		&itemState, &currentSnapshotID, &targetID, &platformVersion, &coreID, &artifactID, &activeDATID,
	); err != nil || itemState != "REVIEW_PENDING" || currentSnapshotID != candidate.baseSnapshotID ||
		artifactID != candidate.artifactID || !activeDATID.Valid || activeDATID.String != candidate.datID {
		return ErrInvalid
	}
	var jobState, currentWorker string
	if err := transaction.QueryRowContext(ctx, `SELECT state,worker_id FROM jobs WHERE id=?`, jobID).
		Scan(&jobState, &currentWorker); err != nil || jobState != "RUNNING" || currentWorker != workerID {
		return ErrInvalid
	}
	newSnapshotID, _ := uuid.NewV7()
	validationID, _ := uuid.NewV7()
	var revision int
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(revision_no),0)+1 FROM import_item_source_snapshots WHERE import_item_id=?
	`, candidate.itemID).Scan(&revision); err != nil {
		return parentStoreError("allocate source revision", err)
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(id,import_item_id,revision_no,source_manifest_json,
source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,?,?,?,'ARCADE_PARENT_ATTACHMENT',?)
	`, newSnapshotID.String(), candidate.itemID, revision, manifestJSON, manifestDigest, now); err != nil {
		return parentStoreError("insert source snapshot", err)
	}
	for _, file := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,
upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?)
`, newSnapshotID.String(), file.role, file.logicalName, file.uploadFileID, file.blobID,
			nullable(file.archiveBlobID), nullableInt(file.archiveOrdinal), file.sortOrder, now); err != nil {
			return parentStoreError("insert source snapshot file", err)
		}
	}
	for _, entry := range entries {
		if _, err := transaction.ExecContext(ctx, `
INSERT OR IGNORE INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,
ascii_casefold_path,archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
materialized_blob_id,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,?)
`, candidate.blobID, entry.Ordinal, entry.OriginalPath, entry.NormalizedPath, entry.ASCIICasefoldPath,
			entry.ArchiveFormat, entry.CompressionProfile, entry.Size, entry.CRC32, entry.MD5, entry.SHA1,
			entry.SHA256, now); err != nil {
			return parentStoreError("insert source archive entry", err)
		}
	}
	validationInput, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "validatorVersion": "arcade-source-validator-v3",
		"sourceSnapshotId": newSnapshotID.String(), "sourceManifestDigest": manifestDigest,
		"targetPlatformInstanceId": targetID, "platformInstanceVersion": platformVersion,
		"coreArtifactId": artifactID, "datVersionId": candidate.datID,
		"dependencySnapshot": json.RawMessage(validation.dependencySnapshot), "status": validation.validationStatus,
	})
	validationDigest := sha256.Sum256(validationInput)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,core_artifact_id,dat_version_id,default_dos_entry,
source_manifest_digest,source_snapshot_id,prepublish_input_digest,status,compatibility_code,
dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,?,?,?,NULL,?,?,?,?,?,?,?)
`, validationID.String(), candidate.itemID, targetID, platformVersion, coreID, artifactID, candidate.datID,
		manifestDigest, newSnapshotID.String(), hex.EncodeToString(validationDigest[:]), validation.validationStatus,
		validation.compatibilityCode, validation.dependencySnapshot, now); err != nil {
		return parentStoreError("insert source validation", err)
	}
	for _, file := range validation.validationFiles {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
sort_order,created_at_ms) VALUES(?,?,?,?,?,?)
	`, validationID.String(), file.role, file.logicalName, file.blobID, file.sortOrder, now); err != nil {
			return parentStoreError("insert validation file", err)
		}
	}
	diagnosticsJSON, _ := json.Marshal(diagnostics)
	selectedValidation := any(nil)
	if validation.validationStatus == "READY" {
		selectedValidation = validationID.String()
	}
	consumptionID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
	evidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": candidate.attachmentID, "machine": candidate.machine,
		"originalFilename": candidate.originalName, "observedSizeBytes": candidate.blobSize,
		"observedSha256": candidate.blobSHA, "baseSourceSnapshotId": candidate.baseSnapshotID,
		"resultSourceSnapshotId": newSnapshotID.String(), "validationId": validationID.String(),
		"validationStatus": validation.validationStatus, "state": "ACCEPTED",
	})
	result, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='ACCEPTED',accepted_blob_id=?,
result_source_snapshot_id=?,observed_size_bytes=?,observed_sha256=?,diagnostics_json=?,error_code=NULL,
finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, candidate.blobID, newSnapshotID.String(), candidate.blobSize, candidate.blobSHA,
		string(diagnosticsJSON), now, now, candidate.attachmentID)
	if err != nil {
		return parentStoreError("accept attachment", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,?,'REVIEW_ARCADE_PARENT',?,?)
	`, consumptionID.String(), candidate.uploadSessionID, candidate.uploadFileID,
		candidate.attachmentID, now); err != nil {
		return parentStoreError("consume parent upload", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_drafts SET effective_source_snapshot_id=?,selected_validation_id=?,
version=version+1,updated_at_ms=? WHERE id=? AND effective_source_snapshot_id=?
`, newSnapshotID.String(), selectedValidation, now, candidate.draftID, candidate.baseSnapshotID)
	if err != nil {
		return parentStoreError("advance review source", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items SET version=version+1,updated_at_ms=? WHERE id=? AND state='REVIEW_PENDING';
	`, now, candidate.itemID); err != nil {
		return parentStoreError("advance import item", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'PARENT_ATTACHMENT_ACCEPTED',?,?,?,'{}',?,?,'{}',?,'{}',?)
`, eventID.String(), candidate.itemID, reviewActor(ctx).Kind, reviewActor(ctx).UserID, reviewActor(ctx).Label,
		string(evidence), string(evidence), string(evidence), now); err != nil {
		return parentStoreError("record parent review event", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'ARCHIVE_SCANNED','{}',?),
(?,'IMPORT_ITEM',?,'PARENT_MATCHED','{}',?),
(?,'IMPORT_ITEM',?,'SOURCE_SNAPSHOT_CREATED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?),
(?,'IMPORT_ITEM',?,'SUCCEEDED','{}',?)
`, jobID, candidate.itemID, now,
		jobID, candidate.itemID, now,
		jobID, candidate.itemID, fmt.Sprintf(`{"sourceSnapshotId":%q}`, newSnapshotID.String()), now,
		jobID, candidate.itemID,
		fmt.Sprintf(`{"validationId":%q,"status":%q}`, validationID.String(), validation.validationStatus), now,
		jobID, candidate.itemID, now); err != nil {
		return parentStoreError("record accepted job events", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now, jobID, workerID)
	if err != nil {
		return parentStoreError("complete parent job", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	if err := transaction.Commit(); err != nil {
		return parentStoreError("commit accepted attachment", err)
	}
	return nil
}

func (service *Service) finishRejectedParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code, archiveCode string,
	missing, mismatched []string,
) {
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "archiveCode": archiveCode, "missingEntries": missing,
		"mismatchedEntries": mismatched,
	})
	now := service.now().UnixMilli()
	eventID, _ := uuid.NewV7()
	evidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attachmentId": candidate.attachmentID, "machine": candidate.machine,
		"originalFilename": candidate.originalName, "observedSizeBytes": candidate.blobSize,
		"observedSha256": candidate.blobSHA, "baseSourceSnapshotId": candidate.baseSnapshotID,
		"state": "REJECTED", "errorCode": code,
	})
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='REJECTED',error_code=?,diagnostics_json=?,
observed_size_bytes=?,observed_sha256=?,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, code, string(diagnostics), candidate.blobSize, candidate.blobSHA, now, now, candidate.attachmentID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, jobID, workerID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'PARENT_REJECTED',?,?),(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, jobID, candidate.itemID, string(diagnostics), now, jobID, candidate.itemID,
		fmt.Sprintf(`{"errorCode":%q}`, code), now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'PARENT_ATTACHMENT_REJECTED',?,?,?,'{}',?,?,'{}',?,'{}',?)
`, eventID.String(), candidate.itemID, reviewActor(ctx).Kind, reviewActor(ctx).UserID, reviewActor(ctx).Label,
		string(evidence), string(evidence), string(evidence), now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) finishRetryableParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code string,
) {
	now := service.now().UnixMilli()
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='FAILED_RETRYABLE',error_code=?,
diagnostics_json=?,observed_size_bytes=?,observed_sha256=?,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, code, diagnostics, candidate.blobSize, candidate.blobSHA, now, now, candidate.attachmentID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, jobID, workerID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, jobID, candidate.itemID, diagnostics, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) SyncParentAttachmentCancellation(ctx context.Context, jobID string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE')
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='CANCELLED')
`, now, now, jobID, jobID)
}

func (service *Service) finishParentAttachmentCancellation(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) bool {
	var state string
	if err := service.database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND worker_id=?`, jobID, workerID).
		Scan(&state); err != nil || state != "CANCEL_REQUESTED" {
		return false
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, now, candidate.attachmentID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, now, now, jobID, workerID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED','{}',?)
`, jobID, candidate.itemID, now); err != nil {
		return false
	}
	return transaction.Commit() == nil
}

func (service *Service) ReviewArcadeDependencies(ctx context.Context, itemID string) (any, bool, error) {
	var platformID string
	var validationStatus, compatibilityCode, dependencyJSON sql.NullString
	if err := service.database.QueryRowContext(ctx, `
SELECT platform.platform_id,validation.status,validation.compatibility_code,
validation.dependency_snapshot_json
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
LEFT JOIN import_item_core_validations validation ON validation.id=COALESCE(
  draft.selected_validation_id,
  (SELECT candidate.id FROM import_item_core_validations candidate
   WHERE candidate.import_item_id=item.id
   AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
   AND candidate.target_platform_instance_id=draft.target_platform_instance_id
   ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1)
)
	WHERE item.id=? AND item.state='REVIEW_PENDING'
	`, itemID).Scan(&platformID, &validationStatus, &compatibilityCode, &dependencyJSON); err != nil {
		return nil, false, parentStoreError("read review dependency snapshot", err)
	}
	if platformID != "arcade" || !dependencyJSON.Valid {
		return nil, false, nil
	}
	snapshot, err := service.projectArcadeSnapshotV2(ctx, dependencyJSON.String)
	if err != nil {
		return nil, false, err
	}
	attachments, active, err := service.reviewParentAttachments(ctx, itemID)
	if err != nil {
		return nil, false, err
	}
	unsupported := compatibilityCode.String == "UNSUPPORTED_MERGED_ROMSET" ||
		compatibilityCode.String == "UNSUPPORTED_CHD" ||
		compatibilityCode.String == "ARCADE_DEPENDENCY_CYCLE" ||
		compatibilityCode.String == "ARCADE_DAT_UNAVAILABLE"
	nodes := make([]map[string]any, 0, len(snapshot.Dependencies))
	for _, dependency := range snapshot.Dependencies {
		latest := attachments[dependency.Machine]
		canAttach := dependency.Kind == "PARENT" &&
			(dependency.State == "MISSING" || dependency.State == "MISMATCH") && active == nil && !unsupported
		node := map[string]any{
			"kind": dependency.Kind, "machine": dependency.Machine, "requiredBy": dependency.RequiredBy,
			"depth": dependency.Depth, "expectedLogicalName": dependency.ExpectedLogicalName,
			"state": dependency.State, "requiredEntryCount": dependency.RequiredEntryCount,
			"requiredEntries": dependency.RequiredEntries, "canAttach": canAttach, "attachment": latest,
		}
		if dependency.Kind == "BIOS_OR_BASE" {
			node["managementUrl"] = "/admin/bios"
		}
		nodes = append(nodes, node)
	}
	return map[string]any{
		"machine": snapshot.Machine, "status": validationStatus.String,
		"compatibilityCode": compatibilityCode.String, "nodes": nodes, "activeAttachment": active,
	}, true, nil
}

func (service *Service) reviewParentAttachments(
	ctx context.Context,
	itemID string,
) (map[string]any, any, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,dependency_machine,expected_logical_name,original_filename,state,error_code,
job_id,observed_size_bytes,observed_sha256,diagnostics_json,created_at_ms,updated_at_ms,finished_at_ms
FROM review_arcade_parent_attachments
WHERE import_item_id=?
ORDER BY created_at_ms DESC,id DESC
	`, itemID)
	if err != nil {
		return nil, nil, parentStoreError("read review parent attachments", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	byMachine := make(map[string]any)
	var active any
	for rows.Next() {
		var id, machine, logicalName, originalName, state, jobID, diagnosticsJSON string
		var errorCode, observedSHA sql.NullString
		var observedSize, finishedAt sql.NullInt64
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&id, &machine, &logicalName, &originalName, &state, &errorCode, &jobID, &observedSize,
			&observedSHA, &diagnosticsJSON, &createdAt, &updatedAt, &finishedAt,
		); err != nil {
			return nil, nil, parentStoreError("scan review parent attachment", err)
		}
		var diagnostics any
		_ = json.Unmarshal([]byte(diagnosticsJSON), &diagnostics)
		value := map[string]any{
			"attachmentId": id, "machine": machine, "expectedLogicalName": logicalName,
			"originalFilename": originalName, "state": state, "errorCode": nullable(errorCode),
			"jobId": jobID, "observedSizeBytes": nullableInt(observedSize),
			"observedSha256": nullable(observedSHA), "diagnostics": diagnostics,
			"createdAtMs": createdAt, "updatedAtMs": updatedAt, "finishedAtMs": nullableInt(finishedAt),
		}
		if _, exists := byMachine[machine]; !exists {
			byMachine[machine] = value
		}
		if active == nil && (state == "QUEUED" || state == "RUNNING") {
			active = value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, parentStoreError("iterate review parent attachments", err)
	}
	return byMachine, active, nil
}
