package payloadrelease

import (
	"context"
	"fmt"

	application "retrom/internal/model/payloadrelease"
)

func (records effectRecords) Remaining(ctx context.Context, scope application.Scope) (int64, error) {
	switch scope.Type {
	case application.ScopeGame:
		return records.gameRemaining(ctx, scope.ID)
	case application.ScopeImportItem:
		return records.itemRemaining(ctx, scope.ID)
	case application.ScopeImportJob:
		return records.aggregateRemaining(ctx, scope.ID)
	case application.ScopePegasusImportItem, application.ScopeEmulationStationImportItem:
		spec, err := effectSourceSpec(scope.Type)
		if err != nil {
			return 0, err
		}
		query := `SELECT (SELECT count(*) FROM ` + spec.filesTable + `
WHERE item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL))+
(SELECT count(*) FROM ` + spec.assetsTable + ` WHERE item_id=? AND blob_id IS NOT NULL)`
		return records.readCount(ctx, query, scope.ID, scope.ID)
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return 0, application.ErrScopeInvalid
	default:
		return 0, application.ErrScopeInvalid
	}
}

func (records effectRecords) readCount(ctx context.Context, query string, args ...any) (int64, error) {
	var count int64
	if err := records.executor.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("read release reference count: %w", err)
	}
	return count, nil
}

func (records effectRecords) gameRemaining(ctx context.Context, id string) (int64, error) {
	return records.readCount(ctx, `
SELECT
 (SELECT count(*) FROM game_assets WHERE game_id=?)+
 (SELECT count(*) FROM game_files file
  WHERE file.game_id=?)+
 (SELECT count(*) FROM variant_files file
  JOIN game_variants variant ON variant.id=file.game_variant_id
  WHERE variant.game_id=?)+
 (SELECT count(*) FROM save_states WHERE game_id=?)+
 (SELECT count(*) FROM launch_content_files file
  JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?)+
 (SELECT count(*) FROM launch_external_files file
  JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?)+
 (SELECT count(*) FROM content_hash_evidence evidence
  JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
  WHERE run.game_id=? AND evidence.payload_released_at_ms IS NULL)+
 (SELECT count(*) FROM scrape_candidate_assets asset
  JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
  JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.game_id=?)
`, id, id, id, id, id, id, id, id)
}

func (records effectRecords) itemRemaining(ctx context.Context, id string) (int64, error) {
	return records.readCount(ctx, `
SELECT
  (SELECT count(*) FROM import_item_source_files WHERE import_item_id=?)+
  (SELECT count(*) FROM import_item_source_snapshot_files file
   JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
   WHERE snapshot.import_item_id=?)+
  (SELECT count(*) FROM import_item_validation_files file
   JOIN import_item_core_validations validation ON validation.id=file.import_item_core_validation_id
   WHERE validation.import_item_id=?)+
  (SELECT count(*) FROM review_uploaded_assets WHERE import_item_id=?)+
  (SELECT count(*) FROM review_preview_sessions WHERE import_item_id=?)+
  (SELECT count(*) FROM content_hash_evidence evidence
   JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
   WHERE run.import_item_id=? AND evidence.payload_released_at_ms IS NULL)+
  (SELECT count(*) FROM scrape_candidate_assets asset
   JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
   JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.import_item_id=?)+
  (SELECT count(*) FROM pegasus_import_item_files file
   JOIN pegasus_import_items item ON item.id=file.item_id
   WHERE item.library_import_item_id=?
     AND (file.blob_id IS NOT NULL OR file.source_archive_blob_id IS NOT NULL))+
  (SELECT count(*) FROM pegasus_import_item_assets asset
   JOIN pegasus_import_items item ON item.id=asset.item_id
   WHERE item.library_import_item_id=? AND asset.blob_id IS NOT NULL)+
  (SELECT count(*) FROM emulationstation_import_item_files file
   JOIN emulationstation_import_items item ON item.id=file.item_id
   WHERE item.library_import_item_id=?
     AND (file.blob_id IS NOT NULL OR file.source_archive_blob_id IS NOT NULL))+
  (SELECT count(*) FROM emulationstation_import_item_assets asset
   JOIN emulationstation_import_items item ON item.id=asset.item_id
   WHERE item.library_import_item_id=? AND asset.blob_id IS NOT NULL)
`, id, id, id, id, id, id, id, id, id,
		id, id)
}

func (records effectRecords) aggregateRemaining(ctx context.Context, id string) (int64, error) {
	return records.readCount(ctx, `
SELECT (SELECT count(*) FROM import_items WHERE import_job_id=? AND payload_state<>'RELEASED')+
       (SELECT count(*) FROM upload_consumptions
        WHERE consumer_type='IMPORT_JOB' AND consumer_id=? AND released_at_ms IS NULL)
`, id, id)
}
