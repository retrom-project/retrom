package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/persistence/contentquery"
	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"

	"github.com/google/uuid"
)

// ArcadeParentAttachments owns the transaction used while an uploaded parent
// ROM is admitted into the review queue. The legacy libraryimport package
// keeps the domain checks and only supplies typed values to this repository.
type ArcadeParentAttachments struct{ database *sql.DB }

type arcadeParentAttachmentAdmissionRecords struct{ executor dbexec.Executor }

var _ application.ArcadeParentAttachmentAdmissionRepository = (*ArcadeParentAttachments)(nil)

func NewArcadeParentAttachments(database *sql.DB) *ArcadeParentAttachments {
	return &ArcadeParentAttachments{database: database}
}

func (repository *ArcadeParentAttachments) WithAdmission(
	ctx context.Context,
	work func(application.ArcadeParentAttachmentAdmissionScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin arcade parent attachment admission: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := arcadeParentAttachmentAdmissionRecords{executor: tx}
	if err := work(application.ArcadeParentAttachmentAdmissionScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit arcade parent attachment admission: %w", err)
	}
	return nil
}

func (records arcadeParentAttachmentAdmissionRecords) Draft(
	ctx context.Context, itemID string,
) (application.ArcadeParentAttachmentDraft, bool, error) {
	var result application.ArcadeParentAttachmentDraft
	var datID sql.NullString
	err := records.executor.QueryRowContext(ctx, `
SELECT draft.id,item.state,draft.version,draft.target_platform_instance_id,
  draft.effective_source_snapshot_id,platform.platform_id,platform.version,
  platform.default_core_id,target.provider_id,target.target_id,
  `+contentquery.BindingPolicySQL+`,
  (SELECT dat.id FROM dat_versions dat
   WHERE dat.provider_id=target.provider_id AND dat.target_id=target.target_id AND dat.is_active=1)
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
  AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN runtime_target_bindings binding ON binding.core_id=platform.default_core_id
  AND binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=binding.binding_id
  AND platform_binding.platform_id=platform.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id
  AND target.target_id=binding.target_id
WHERE item.id=?
`, itemID).Scan(
		&result.DraftID, &result.ItemState, &result.DraftVersion, &result.TargetID,
		&result.EffectiveSnapshotID, &result.PlatformID, &result.PlatformVersion,
		&result.CoreID, &result.ProviderID, &result.RuntimeTargetID,
		contentquery.ScanPolicy(&result.ContentPolicy), &datID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ArcadeParentAttachmentDraft{}, false, nil
	}
	if err != nil {
		return application.ArcadeParentAttachmentDraft{}, false, fmt.Errorf("read arcade parent draft: %w", err)
	}
	result.ActiveDATVersionID, result.HasActiveDAT = nullableString(datID)
	return result, true, nil
}

func (records arcadeParentAttachmentAdmissionRecords) Validation(
	ctx context.Context, validationID, itemID string,
) (application.ArcadeParentAttachmentValidation, bool, error) {
	var result application.ArcadeParentAttachmentValidation
	var datID sql.NullString
	err := records.executor.QueryRowContext(ctx, `
SELECT target_platform_instance_id,core_id,provider_id,target_id,
  dat_version_id,source_snapshot_id,
  dependency_snapshot_json
FROM import_item_core_validations
WHERE id=? AND import_item_id=?
`, validationID, itemID).Scan(
		&result.TargetPlatformInstanceID, &result.CoreID, &result.ProviderID, &result.TargetID,
		&datID, &result.SourceSnapshotID, &result.DependencySnapshotJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ArcadeParentAttachmentValidation{}, false, nil
	}
	if err != nil {
		return application.ArcadeParentAttachmentValidation{}, false, fmt.Errorf("read arcade parent validation: %w", err)
	}
	result.DATVersionID, result.HasDATVersion = nullableString(datID)
	return result, true, nil
}

func (records arcadeParentAttachmentAdmissionRecords) Upload(
	ctx context.Context, uploadFileID string,
) (application.ArcadeParentAttachmentUpload, bool, error) {
	var result application.ArcadeParentAttachmentUpload
	var wholeSessionConsumed int64
	err := records.executor.QueryRowContext(ctx, `
SELECT session.id,session.state,file.state,file.relative_path,file.final_blob_id,
  blob.sha256,blob.size_bytes,
  EXISTS(SELECT 1 FROM upload_consumptions consumption
    WHERE consumption.upload_session_id=session.id AND consumption.upload_file_id IS NULL)
FROM upload_files file
JOIN upload_sessions session ON session.id=file.upload_session_id
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.id=?
`, uploadFileID).Scan(
		&result.UploadSessionID, &result.SessionState, &result.FileState, &result.RelativePath,
		&result.BlobID, &result.BlobSHA, &result.BlobSize, &wholeSessionConsumed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ArcadeParentAttachmentUpload{}, false, nil
	}
	if err != nil {
		return application.ArcadeParentAttachmentUpload{}, false, fmt.Errorf("read arcade parent upload: %w", err)
	}
	result.WholeSessionConsumed = wholeSessionConsumed != 0
	return result, true, nil
}

func (records arcadeParentAttachmentAdmissionRecords) HasActive(ctx context.Context, itemID string) (bool, error) {
	var count int64
	if err := records.executor.QueryRowContext(ctx, `
SELECT count(*) FROM review_arcade_parent_attachments
WHERE import_item_id=? AND state IN ('QUEUED','RUNNING')
`, itemID).Scan(&count); err != nil {
		return false, fmt.Errorf("read active arcade parent attachment: %w", err)
	}
	return count != 0, nil
}

func (records arcadeParentAttachmentAdmissionRecords) MachineRelation(
	ctx context.Context, datID, machine string,
) (application.ArcadeMachineRelation, bool, error) {
	return BindArcadeRelations(records.executor).MachineRelation(ctx, datID, machine)
}

func (records arcadeParentAttachmentAdmissionRecords) Create(
	ctx context.Context, write application.ArcadeParentAttachmentWrite,
) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,
  state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'IMPORT_ITEM',?,'REVIEW_ARCADE_PARENT_VALIDATE',?,1,?,1,'QUEUED',0,4,?,?,?)
`, write.JobID, write.ItemID, write.DedupeKey, write.InputJSON, write.NowMS, write.NowMS, write.NowMS); err != nil {
		return fmt.Errorf("create arcade parent job: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, write.JobID, write.InputJSON, write.InputDigest, write.NowMS); err != nil {
		return fmt.Errorf("create arcade parent input snapshot: %w", err)
	}
	_, err := recordstore.CreateReviewArcadeParentAttachments(ctx, records.executor, `
INSERT INTO review_arcade_parent_attachments(
  id,import_item_id,review_draft_id,base_source_snapshot_id,dependency_machine,
  expected_logical_name,required_by_machine,depth,provider_id,target_id,dat_version_id,
  upload_file_id,original_filename,state,diagnostics_json,job_id,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'QUEUED','{}',?,1,?,?)
`, write.AttachmentID, write.ItemID, write.DraftID, write.BaseSourceSnapshotID,
		write.DependencyMachine, write.DependencyMachine+".zip", write.RequiredByMachine,
		write.Depth, write.ProviderID, write.TargetID, write.DATVersionID, write.UploadID,
		write.OriginalFilename, write.JobID, write.NowMS, write.NowMS)
	if err != nil {
		if strings.Contains(err.Error(), "review_arcade_parent_active") {
			return fmt.Errorf("%w: %w", application.ErrArcadeParentAttachmentActive, err)
		}
		return fmt.Errorf("create arcade parent attachment: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{}',?)
`, write.JobID, write.ItemID, write.NowMS); err != nil {
		return fmt.Errorf("record arcade parent queue event: %w", err)
	}
	result, err := recordstore.UpdateReviewDrafts(ctx, records.executor, recordstore.Update{
		Set:    `version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND version=?`, Args: []any{write.DraftID, write.ExpectedDraftVersion}},
		Values: []any{write.NowMS},
	})
	if err != nil {
		return fmt.Errorf("advance arcade parent draft: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count arcade parent draft update: %w", err)
	}
	if changed != 1 {
		return application.ErrVersionConflict
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("allocate arcade parent review event: %w", err)
	}
	if _, err := recordstore.CreateReviewEvents(ctx, records.executor, `
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
  after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms
) VALUES(?,?,'PARENT_UPLOAD_REQUESTED',?,?,?,?,?,?,?,?,?,?)
`, eventID.String(), write.ItemID, write.Actor.Kind, write.Actor.UserID, write.Actor.Label,
		emptyArcadeParentReviewEvent, write.EvidenceJSON, write.EvidenceJSON,
		emptyArcadeParentReviewEvent, emptyArcadeParentReviewEvent, emptyArcadeParentReviewEvent,
		write.NowMS); err != nil {
		return fmt.Errorf("record arcade parent review event: %w", err)
	}
	return nil
}

const emptyArcadeParentReviewEvent = `{"schemaVersion":2}`

func nullableString(value sql.NullString) (string, bool) {
	if !value.Valid {
		return "", false
	}
	return value.String, true
}
