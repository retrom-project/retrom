package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/content/arcade"
	arcaderecords "retrom/internal/persistence/arcade"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

// ArcadeParentAttachments owns the transaction used while an uploaded parent
// ROM is admitted into the review queue. The legacy libraryimport package
// keeps the domain checks and only supplies typed values to this repository.
type ArcadeParentAttachments struct{ database dbapi.DB }

type arcadeParentAttachmentAdmissionRecords struct{ executor dbapi.Executor }

var _ libraryservice.ArcadeParentAttachmentAdmissionRepository = (*ArcadeParentAttachments)(nil)

func NewArcadeParentAttachments(database dbapi.DB) *ArcadeParentAttachments {
	return &ArcadeParentAttachments{database: database}
}

func (repository *ArcadeParentAttachments) WithAdmission(
	ctx context.Context,
	work func(libraryservice.ArcadeParentAttachmentAdmissionScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin arcade parent attachment admission: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := arcadeParentAttachmentAdmissionRecords{executor: tx}
	if err := work(libraryservice.ArcadeParentAttachmentAdmissionScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit arcade parent attachment admission: %w", err)
	}
	return nil
}

func (records arcadeParentAttachmentAdmissionRecords) Draft(
	ctx context.Context, itemID string,
) (libraryservice.ArcadeParentAttachmentDraft, bool, error) {
	var result libraryservice.ArcadeParentAttachmentDraft
	var datID sql.NullString
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT draft.id,item.state,draft.review_version,draft.target_platform_instance_id,
  draft.effective_source_snapshot_id,platform.platform_id,platform.version,
  platform.default_core_id,target.provider_id,target.target_id,
  `+contentquery.BindingPolicySQL+`,
  (SELECT dat.id FROM dat_versions dat
   WHERE dat.provider_id=target.provider_id AND dat.target_id=target.target_id AND dat.is_active=1)
FROM import_items item
JOIN import_items draft ON draft.id=item.id
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
		return libraryservice.ArcadeParentAttachmentDraft{}, false, nil
	}
	if err != nil {
		return libraryservice.ArcadeParentAttachmentDraft{}, false, fmt.Errorf("read arcade parent draft: %w", err)
	}
	result.ActiveDATVersionID, result.HasActiveDAT = nullableString(datID)
	return result, true, nil
}

func (records arcadeParentAttachmentAdmissionRecords) Validation(
	ctx context.Context, itemID string,
) (libraryservice.ArcadeParentAttachmentValidation, bool, error) {
	runtime, err := ReadReviewRuntime(ctx, records.executor, itemID)
	if err != nil {
		return libraryservice.ArcadeParentAttachmentValidation{}, false, err
	}
	result := libraryservice.ArcadeParentAttachmentValidation{
		TargetPlatformInstanceID: runtime.PlatformInstanceID, CoreID: runtime.CoreID,
		ProviderID: runtime.ProviderID, TargetID: runtime.TargetID,
		SourceSnapshotID: runtime.SnapshotID, DependencySnapshotJSON: runtime.DependencyJSON,
	}
	if runtime.DATID != nil {
		result.DATVersionID = *runtime.DATID
		result.HasDATVersion = true
	}
	return result, runtime.SnapshotID != "", nil
}

func (records arcadeParentAttachmentAdmissionRecords) Upload(
	ctx context.Context, uploadFileID string,
) (libraryservice.ArcadeParentAttachmentUpload, bool, error) {
	var result libraryservice.ArcadeParentAttachmentUpload
	var wholeSessionConsumed int64
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT session.id,session.state,'COMPLETE',file.relative_path,file.file_record,
  json_extract(blob.value, '$.sha256'),json_extract(blob.value, '$.size_bytes'),
  EXISTS(SELECT 1 FROM upload_consumptions consumption
    WHERE consumption.upload_session_id=session.id AND consumption.upload_file_id IS NULL)
FROM import_files file
JOIN upload_sessions session ON session.id=file.upload_session_id
JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
WHERE file.id=?
`, uploadFileID).Scan(
		&result.UploadSessionID, &result.SessionState, &result.FileState, &result.RelativePath,
		&result.FileRecord, &result.BlobSHA, &result.BlobSize, &wholeSessionConsumed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ArcadeParentAttachmentUpload{}, false, nil
	}
	if err != nil {
		return libraryservice.ArcadeParentAttachmentUpload{}, false, fmt.Errorf("read arcade parent upload: %w", err)
	}
	result.WholeSessionConsumed = wholeSessionConsumed != 0
	return result, true, nil
}

func (records arcadeParentAttachmentAdmissionRecords) HasActive(ctx context.Context, itemID string) (bool, error) {
	var count int64
	if err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT count(*) FROM review_arcade_parent_attachments
WHERE import_item_id=? AND state='PENDING'
`, itemID).Scan(&count); err != nil {
		return false, fmt.Errorf("read active arcade parent attachment: %w", err)
	}
	return count != 0, nil
}

func (records arcadeParentAttachmentAdmissionRecords) MachineRelation(
	ctx context.Context, datID, machine string,
) (arcade.MachineRelation, bool, error) {
	relation, found, err := arcaderecords.New(records.executor).MachineRelation(ctx, datID, machine)
	if err != nil {
		return arcade.MachineRelation{}, false, fmt.Errorf("read parent admission relation: %w", err)
	}
	return relation, found, nil
}

func (records arcadeParentAttachmentAdmissionRecords) Create(
	ctx context.Context, write libraryservice.ArcadeParentAttachmentWrite,
) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,
  state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'IMPORT_ITEM',?,'REVIEW_ARCADE_PARENT_VALIDATE',?,1,?,1,'QUEUED',0,4,?,?,?)
`, write.JobID, write.ItemID, write.DedupeKey,
		`{"schemaVersion":1,"inputExecutionNo":1}`, write.NowMS, write.NowMS, write.NowMS); err != nil {
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
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'PENDING','{}',?,1,?,?)
`, write.AttachmentID, write.ItemID, write.DraftID, write.BaseSourceSnapshotID,
		write.DependencyMachine, write.DependencyMachine+".zip", write.RequiredByMachine,
		write.Depth, write.ProviderID, write.TargetID, write.DATVersionID, write.UploadID,
		write.OriginalFilename, write.JobID, write.NowMS, write.NowMS)
	if err != nil {
		if strings.Contains(err.Error(), "review_arcade_parent_active") {
			return fmt.Errorf("%w: %w", libraryservice.ErrArcadeParentAttachmentActive, err)
		}
		return fmt.Errorf("create arcade parent attachment: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{}',?)
`, write.JobID, write.ItemID, write.NowMS); err != nil {
		return fmt.Errorf("record arcade parent queue event: %w", err)
	}
	result, err := recordstore.UpdateReviewItems(ctx, records.executor, recordstore.Update{
		Set: `review_version=review_version+1,review_updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND review_version=?`,
			Args:  []any{write.DraftID, write.ExpectedDraftVersion},
		},
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
		return libraryservice.ErrVersionConflict
	}

	return nil
}

func nullableString(value sql.NullString) (string, bool) {
	if !value.Valid {
		return "", false
	}
	return value.String, true
}
