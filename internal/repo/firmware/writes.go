package firmware

import (
	"context"
	"fmt"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/model/firmware"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/recordstore"
)

func (store writes) Ensure(ctx context.Context, metadata blobstore.Metadata, now int64) (string, error) {
	id, err := blobcatalog.EnsureRecord(ctx, store.transaction, metadata, "application/octet-stream", now)
	if err != nil {
		return "", fmt.Errorf("register BIOS blob: %w", err)
	}
	return id, nil
}

func (store writes) Create(ctx context.Context, value firmware.InstallationWrite) error {
	return changed(
		recordstore.CreateBiosInstallations(
			ctx,
			store.transaction,
			`
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
 validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms,
 source_kind,server_import_candidate_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,1,1,?,?,?,?)`,

			value.ID,
			value.RequirementID,
			value.BlobID,
			value.Filename,
			value.Size,
			value.MD5,
			value.SHA1,
			value.SHA256,

			value.RequirementVersion,
			value.Status,
			string(
				value.DetailsJSON,
			),
			value.AtMS,
			value.AtMS,
			value.SourceKind,
			value.CandidateID,
		),
	)
}

func (store writes) Consume(ctx context.Context, value firmware.Consumption) error {
	return changed(recordstore.CreateUploadConsumptions(ctx, store.transaction, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,?,'BIOS_INSTALLATION',?,?)`, value.ID, value.UploadID, value.FileID, value.InstallationID, value.AtMS))
}

func (store writes) SelectCandidate(ctx context.Context, value firmware.Selection) error {
	return changed(store.transaction.ExecContext(ctx, `UPDATE server_bios_import_candidates
SET state='SELECTED',not_selected_reason=NULL,updated_at_ms=?
WHERE id=? AND server_import_id=? AND requirement_id=? AND state IN ('ELIGIBLE','SELECTED')`,
		value.AtMS, value.CandidateID, value.ImportID, value.RequirementID))
}

func (store writes) Finish(ctx context.Context, value firmware.ServerOutcome) error {
	if err := changed(recordstore.UpdateServerBiosImportItems(ctx, store.transaction, recordstore.Update{
		Set: `state=?,match_method=?,selection_details_json=?,outcome_code=?,previous_installation_id=?,
new_installation_id=?,completed_at_ms=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')`,
			Args:  []any{value.ImportID, value.RequirementID},
		},
		Values: []any{
			value.Result.Outcome, value.MatchMethod, string(value.DetailsJSON), value.Code,
			nullable(value.Result.PreviousInstallationID), nullable(value.Result.NewInstallationID), value.AtMS, value.AtMS,
		},
	})); err != nil {
		return err
	}
	if _, err := store.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)`,
		value.JobID,
		value.ImportID,
		string(
			value.EventJSON,
		),
		value.AtMS,
	); err != nil {
		return fmt.Errorf("record BIOS outcome event: %w", err)
	}
	return nil
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
