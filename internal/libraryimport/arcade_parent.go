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
	CoreArtifactVersion  int64  `json:"coreArtifactVersion"`
	CompatibilityDigest  string `json:"compatibilityConfigDigest"`
	DATVersionID         string `json:"datVersionId"`
	UploadFileID         string `json:"uploadFileId"`
}

type parentAttachmentCandidate struct {
	attachmentID, itemID, draftID, baseSnapshotID string
	machine, requiredBy, artifactID, datID        string
	uploadFileID, uploadSessionID, originalName   string
	blobID, blobSHA                               string
	blobSize                                      int64
	artifactVersion                               int64
	compatibilityDigest                           string
	depth                                         int
}

// Preconditions intentionally share one transaction and one stable error mapping.
func (service *Service) CreateArcadeParentAttachment(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	request ParentAttachmentRequest,
) (ParentAttachmentCreated, error) {
	if invalidParentAttachmentRequest(service, itemID, expectedVersion, request) {
		return ParentAttachmentCreated{}, parentError(ParentErrorInvalid, ErrInvalid)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	defer cleanup.Rollback(transaction)
	setup := parentAttachmentSetup{
		service: service, ctx: ctx, transaction: transaction,
		itemID: itemID, expectedVersion: expectedVersion, request: request,
	}
	if err := setup.load(); err != nil {
		return ParentAttachmentCreated{}, err
	}
	result, err := setup.persist()
	if err != nil {
		return ParentAttachmentCreated{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ParentAttachmentCreated{}, parentError(ParentErrorUnavailable, err)
	}
	go service.runParentAttachment(context.WithoutCancel(ctx), result.JobID)
	return result, nil
}

func invalidParentAttachmentRequest(
	service *Service,
	itemID string,
	expectedVersion int64,
	request ParentAttachmentRequest,
) bool {
	return expectedVersion < 1 || itemID == "" || request.ValidationID == "" ||
		request.BaseSourceSnapshotID == "" || request.UploadFileID == "" ||
		!validArcadeMachine(request.DependencyMachine) || service.blobs == nil
}

type parentAttachmentSetup struct {
	service             *Service
	ctx                 context.Context
	transaction         *sql.Tx
	itemID              string
	expectedVersion     int64
	request             ParentAttachmentRequest
	draftID             string
	targetID            string
	effectiveSnapshotID string
	platformID          string
	coreID              string
	artifactID          string
	compatibilityConfig string
	activeDATID         sql.NullString
	platformVersion     int64
	artifactVersion     int64
	dependency          arcadeDraftDependency
	uploadSessionID     string
	originalName        string
	blobID              string
	blobSHA             string
	blobSize            int64
}

func (setup *parentAttachmentSetup) load() error {
	steps := []func() error{
		setup.loadDraft, setup.validateSelectedValidation,
		setup.loadUpload, setup.ensureNoActiveAttachment,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (setup *parentAttachmentSetup) loadDraft() error {
	var itemState string
	var draftVersion int64
	err := setup.transaction.QueryRowContext(setup.ctx, `
SELECT draft.id,item.state,draft.version,draft.target_platform_instance_id,
  draft.effective_source_snapshot_id,platform.platform_id,platform.version,
  platform.default_core_id,artifact.id,artifact.version,artifact.compatibility_json,
  (SELECT dat.id FROM dat_versions dat
   WHERE dat.core_artifact_id=artifact.id AND dat.is_active=1)
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
  AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.selected_for_new_bindings=1
WHERE item.id=?
`, setup.itemID).Scan(
		&setup.draftID, &itemState, &draftVersion, &setup.targetID, &setup.effectiveSnapshotID,
		&setup.platformID, &setup.platformVersion, &setup.coreID, &setup.artifactID,
		&setup.artifactVersion, &setup.compatibilityConfig, &setup.activeDATID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return parentError(ParentErrorNotFound, err)
	}
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	if itemState != "REVIEW_PENDING" {
		return parentError(ParentErrorFinalized, ErrInvalid)
	}
	if draftVersion != setup.expectedVersion {
		return parentError(ParentErrorVersion, ErrInvalid)
	}
	if setup.platformID != "arcade" ||
		setup.effectiveSnapshotID != setup.request.BaseSourceSnapshotID || !setup.activeDATID.Valid {
		return parentError(ParentErrorInputStale, ErrInvalid)
	}
	return nil
}

func (setup *parentAttachmentSetup) validateSelectedValidation() error {
	var targetID, snapshotID, coreID, artifactID string
	var datID sql.NullString
	var platformVersion, artifactVersion, generation int64
	var dependencyJSON string
	err := setup.transaction.QueryRowContext(setup.ctx, `
SELECT target_platform_instance_id,platform_instance_version,core_id,core_artifact_id,
  core_artifact_version,prepublish_generation,dat_version_id,source_snapshot_id,
  dependency_snapshot_json
FROM import_item_core_validations
WHERE id=? AND import_item_id=?
`, setup.request.ValidationID, setup.itemID).Scan(
		&targetID, &platformVersion, &coreID, &artifactID, &artifactVersion,
		&generation, &datID, &snapshotID, &dependencyJSON,
	)
	if err != nil {
		return parentError(ParentErrorInputStale, err)
	}
	if !setup.validationMatches(
		targetID, snapshotID, coreID, artifactID, datID,
		platformVersion, artifactVersion, generation,
	) {
		return parentError(ParentErrorInputStale, ErrInvalid)
	}
	snapshot, err := setup.service.canonicalArcadeSnapshotWithQueryer(
		setup.ctx, setup.transaction, dependencyJSON,
	)
	if err != nil {
		return parentError(ParentErrorInputStale, err)
	}
	dependency, found := attachmentDependency(snapshot, setup.request.DependencyMachine)
	if !found || dependency.Kind != "PARENT" ||
		(dependency.State != "MISSING" && dependency.State != "MISMATCH") {
		return parentError(ParentErrorNotRequired, ErrInvalid)
	}
	if dependency.RequiredBy == nil || dependency.Depth < 1 || dependency.Depth > 63 {
		return parentError(ParentErrorStructure, ErrInvalid)
	}
	setup.dependency = dependency
	return nil
}

func (setup *parentAttachmentSetup) validationMatches(
	targetID, snapshotID, coreID, artifactID string,
	datID sql.NullString,
	platformVersion, artifactVersion, generation int64,
) bool {
	return targetID == setup.targetID && platformVersion == setup.platformVersion &&
		coreID == setup.coreID && artifactID == setup.artifactID &&
		snapshotID == setup.effectiveSnapshotID && artifactVersion == setup.artifactVersion &&
		generation == prepublishGeneration && datID.Valid && datID.String == setup.activeDATID.String
}

func (setup *parentAttachmentSetup) loadUpload() error {
	var uploadState, fileState string
	var wholeSessionConsumed int64
	err := setup.transaction.QueryRowContext(setup.ctx, `
SELECT session.id,session.state,file.state,file.relative_path,file.final_blob_id,
  blob.sha256,blob.size_bytes,
  EXISTS(SELECT 1 FROM upload_consumptions consumption
    WHERE consumption.upload_session_id=session.id AND consumption.upload_file_id IS NULL)
FROM upload_files file
JOIN upload_sessions session ON session.id=file.upload_session_id
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.id=?
`, setup.request.UploadFileID).Scan(
		&setup.uploadSessionID, &uploadState, &fileState, &setup.originalName,
		&setup.blobID, &setup.blobSHA, &setup.blobSize, &wholeSessionConsumed,
	)
	if err != nil || uploadState != "COMPLETE" || fileState != "COMPLETE" ||
		wholeSessionConsumed != 0 || !strings.EqualFold(filepath.Ext(setup.originalName), ".zip") {
		return parentError(ParentErrorInvalid, err)
	}
	info, err := os.Stat(setup.service.blobs.Path(setup.blobSHA))
	if err != nil || !info.Mode().IsRegular() || info.Size() != setup.blobSize {
		return parentError(ParentErrorInvalid, err)
	}
	return nil
}

func (setup *parentAttachmentSetup) ensureNoActiveAttachment() error {
	var count int
	err := setup.transaction.QueryRowContext(setup.ctx, `
SELECT count(*) FROM review_arcade_parent_attachments
WHERE import_item_id=? AND state IN ('QUEUED','RUNNING')
`, setup.itemID).Scan(&count)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	if count != 0 {
		return parentError(ParentErrorInProgress, ErrInvalid)
	}
	return nil
}

func (setup *parentAttachmentSetup) persist() (ParentAttachmentCreated, error) {
	attachmentID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	input := setup.input(attachmentID.String())
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	now := setup.service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte(strings.Join([]string{
		setup.itemID, setup.effectiveSnapshotID, setup.dependency.Machine,
		setup.blobSHA, setup.request.ValidationID,
	}, "\x00")))
	if err := setup.insertJob(jobID.String(), inputJSON, inputDigest, dedupe, now); err != nil {
		return ParentAttachmentCreated{}, err
	}
	if err := setup.insertAttachment(attachmentID.String(), jobID.String(), now); err != nil {
		return ParentAttachmentCreated{}, err
	}
	if err := setup.advanceDraftAndRecordEvent(attachmentID.String(), now); err != nil {
		return ParentAttachmentCreated{}, err
	}
	return ParentAttachmentCreated{
		AttachmentID: attachmentID.String(), State: "QUEUED",
		JobID: jobID.String(), Version: setup.expectedVersion + 1,
	}, nil
}

func (setup *parentAttachmentSetup) input(attachmentID string) parentAttachmentInput {
	return parentAttachmentInput{
		SchemaVersion: 1, AttachmentID: attachmentID, ImportItemID: setup.itemID,
		ReviewDraftID: setup.draftID, BaseSourceSnapshotID: setup.effectiveSnapshotID,
		DependencyMachine: setup.dependency.Machine, CoreArtifactID: setup.artifactID,
		CoreArtifactVersion: setup.artifactVersion,
		CompatibilityDigest: compatibilityConfigDigest(setup.compatibilityConfig),
		DATVersionID:        setup.activeDATID.String, UploadFileID: setup.request.UploadFileID,
	}
}

func (setup *parentAttachmentSetup) insertJob(
	jobID string,
	inputJSON []byte,
	inputDigest [32]byte,
	dedupe [32]byte,
	now int64,
) error {
	_, err := setup.transaction.ExecContext(setup.ctx, `
INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,
  state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'IMPORT_ITEM',?,'REVIEW_ARCADE_PARENT_VALIDATE',?,1,?,1,'QUEUED',0,4,?,?,?)
`, jobID, setup.itemID, hex.EncodeToString(dedupe[:]), string(inputJSON), now, now, now)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	_, err = setup.transaction.ExecContext(setup.ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID, string(inputJSON), hex.EncodeToString(inputDigest[:]), now)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	return nil
}

func (setup *parentAttachmentSetup) insertAttachment(attachmentID, jobID string, now int64) error {
	_, err := setup.transaction.ExecContext(setup.ctx, `
INSERT INTO review_arcade_parent_attachments(
  id,import_item_id,review_draft_id,base_source_snapshot_id,dependency_machine,
  expected_logical_name,required_by_machine,depth,core_artifact_id,dat_version_id,
  upload_file_id,original_filename,state,diagnostics_json,job_id,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'QUEUED','{}',?,1,?,?)
`, attachmentID, setup.itemID, setup.draftID, setup.effectiveSnapshotID,
		setup.dependency.Machine, setup.dependency.Machine+".zip", *setup.dependency.RequiredBy,
		setup.dependency.Depth, setup.artifactID, setup.activeDATID.String,
		setup.request.UploadFileID, filepath.Base(setup.originalName), jobID, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "review_arcade_parent_active") {
			return parentError(ParentErrorInProgress, err)
		}
		return parentError(ParentErrorUnavailable, err)
	}
	_, err = setup.transaction.ExecContext(setup.ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{}',?)
`, jobID, setup.itemID, now)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	return nil
}

func (setup *parentAttachmentSetup) advanceDraftAndRecordEvent(
	_ string,
	now int64,
) error {
	result, err := setup.transaction.ExecContext(setup.ctx, `
UPDATE review_drafts SET version=version+1,updated_at_ms=?
WHERE id=? AND version=?
`, now, setup.draftID, setup.expectedVersion)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return parentError(ParentErrorVersion, ErrInvalid)
	}
	eventID, _ := uuid.NewV7()
	evidence := marshalReviewEventV2(map[string]any{
		"attachmentKind": "ARCADE_PARENT", "machine": setup.dependency.Machine,
		"originalFilename": filepath.Base(setup.originalName), "state": "QUEUED",
	})
	actor := reviewActor(setup.ctx)
	_, err = setup.transaction.ExecContext(setup.ctx, `
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
  after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms
) VALUES(?,?,'PARENT_UPLOAD_REQUESTED',?,?,?,?,?,?,?,?,?,?)
`, eventID.String(), setup.itemID, actor.Kind, actor.UserID, actor.Label,
		emptyReviewEventV2, evidence, evidence, emptyReviewEventV2, emptyReviewEventV2,
		emptyReviewEventV2, now)
	if err != nil {
		return parentError(ParentErrorUnavailable, err)
	}
	return nil
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

func (service *Service) canonicalArcadeSnapshot(
	ctx context.Context,
	raw string,
) (arcadeDraftSnapshot, error) {
	return service.canonicalArcadeSnapshotWithQueryer(ctx, service.database, raw)
}

func (service *Service) canonicalArcadeSnapshotWithQueryer(
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
