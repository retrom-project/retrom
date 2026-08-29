package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
)

func gameBlobIDs(ctx context.Context, transaction *sql.Tx, gameID string) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT blob_id FROM game_assets WHERE game_id=?
UNION ALL SELECT file.blob_id FROM game_content_files file
 JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
 WHERE revision.game_id=?
UNION ALL SELECT file.source_archive_blob_id FROM game_content_files file
 JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
 WHERE revision.game_id=?
UNION ALL SELECT file.blob_id FROM variant_files file
 JOIN game_variant_revisions revision ON revision.id=file.game_variant_revision_id
 JOIN game_variants variant ON variant.id=revision.game_variant_id WHERE variant.game_id=?
UNION ALL SELECT payload_blob_id FROM save_states WHERE game_id=?
UNION ALL SELECT screenshot_blob_id FROM save_states WHERE game_id=?
UNION ALL SELECT file.blob_id FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
UNION ALL SELECT file.blob_id FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
UNION ALL SELECT evidence.blob_id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.game_id=?
UNION ALL SELECT evidence.archive_blob_id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.game_id=?
UNION ALL SELECT asset.blob_id FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.game_id=?
`, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID)
}

func gameConsumptionSessions(ctx context.Context, transaction *sql.Tx, gameID string) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT upload_session_id FROM upload_consumptions WHERE
consumer_type='GAME_ASSET' AND consumer_id IN (SELECT id FROM game_assets WHERE game_id=?) OR
consumer_type='GAME_FILE_REVISION_JOB' AND consumer_id IN (
  SELECT source_ref_id FROM game_content_revisions WHERE game_id=? AND source_kind='ADMIN_REPLACE'
)
`, gameID, gameID)
}

func (service *Service) releaseGameSources(ctx context.Context, transaction *sql.Tx, gameID string, now int64) error {
	importItems, err := collectIDs(ctx, transaction, `
SELECT source_ref_id FROM game_content_revisions WHERE game_id=? AND source_kind='IMPORT_REVIEW'
UNION SELECT source_ref_id FROM game_metadata_revisions WHERE game_id=? AND source_kind='IMPORT_REVIEW'
`, gameID, gameID)
	if err != nil {
		return err
	}
	if err := service.releaseGameImportSources(ctx, transaction, uniqueStrings(importItems), now); err != nil {
		return err
	}
	pegasusItems, err := collectIDs(ctx, transaction, `
SELECT source_ref_id FROM game_content_revisions WHERE game_id=? AND source_kind='SERVER_PEGASUS_IMPORT'
UNION SELECT source_ref_id FROM game_metadata_revisions WHERE game_id=? AND source_kind='SERVER_PEGASUS_IMPORT'
`, gameID, gameID)
	if err != nil {
		return err
	}
	if err := service.releaseGamePegasusSources(ctx, transaction, uniqueStrings(pegasusItems), now); err != nil {
		return err
	}
	emulationStationItems, err := collectIDs(ctx, transaction, `
SELECT source_ref_id FROM game_content_revisions
 WHERE game_id=? AND source_kind='SERVER_EMULATIONSTATION_IMPORT'
UNION SELECT source_ref_id FROM game_metadata_revisions
 WHERE game_id=? AND source_kind='SERVER_EMULATIONSTATION_IMPORT'
`, gameID, gameID)
	if err != nil {
		return err
	}
	return service.releaseGameEmulationStationSources(
		ctx, transaction, uniqueStrings(emulationStationItems), now,
	)
}

func (service *Service) releaseGameImportSources(
	ctx context.Context,
	transaction *sql.Tx,
	itemIDs []string,
	now int64,
) error {
	for _, itemID := range itemIDs {
		var state, payloadState string
		var jobID sql.NullString
		if err := transaction.QueryRowContext(ctx, `
SELECT state,payload_state,payload_release_job_id FROM import_items WHERE id=?
`, itemID).Scan(&state, &payloadState, &jobID); err != nil ||
			!terminalImportItem(state) || !jobID.Valid || payloadState == "RETAINED" {
			return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
		}
		if payloadState != "RELEASED" {
			if _, err := transaction.ExecContext(ctx, `
UPDATE import_items SET payload_state='RELEASING',payload_last_error_code=NULL
WHERE id=? AND payload_state='FAILED'
`, itemID); err != nil {
				return fmt.Errorf("payloadrelease/retry source item: %w", err)
			}
			if err := service.releaseImportItemTx(ctx, transaction, itemID, reasonForImportState(state), now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (service *Service) releaseGamePegasusSources(
	ctx context.Context,
	transaction *sql.Tx,
	itemIDs []string,
	now int64,
) error {
	for _, itemID := range itemIDs {
		var publicItem sql.NullString
		var payloadState string
		if err := transaction.QueryRowContext(ctx, `
SELECT library_import_item_id,payload_state FROM pegasus_import_items WHERE id=?
`, itemID).
			Scan(&publicItem, &payloadState); err != nil {
			return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
		}
		if publicItem.Valid {
			continue
		}
		if payloadState == "RETAINED" {
			return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
		}
		if payloadState != "RELEASED" {
			if err := service.releasePegasusPayload(ctx, transaction, itemID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (service *Service) releaseGameEmulationStationSources(
	ctx context.Context,
	transaction *sql.Tx,
	itemIDs []string,
	now int64,
) error {
	for _, itemID := range itemIDs {
		var publicItem sql.NullString
		var payloadState string
		if err := transaction.QueryRowContext(ctx, `
SELECT library_import_item_id,payload_state FROM emulationstation_import_items WHERE id=?
`, itemID).Scan(&publicItem, &payloadState); err != nil {
			return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
		}
		if publicItem.Valid {
			continue
		}
		if payloadState == "RETAINED" {
			return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
		}
		if payloadState != "RELEASED" {
			if err := service.releaseEmulationStationPayload(ctx, transaction, itemID, now); err != nil {
				return err
			}
		}
	}
	return nil
}
