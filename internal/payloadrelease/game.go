package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/cleanup"
)

func (service *Service) releaseGame(ctx context.Context, job claimedJob) error {
	if err := service.waitForGameMutations(ctx, job.ScopeID); err != nil {
		return err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/game transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	complete, err := ensureGameRelease(ctx, transaction, job, service.now().UnixMilli())
	if err != nil || complete {
		return err
	}
	if err := service.releaseGamePayload(ctx, transaction, job.ScopeID); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/game commit: %w", err)
	}
	return nil
}

func ensureGameRelease(ctx context.Context, transaction *sql.Tx, job claimedJob, now int64) (bool, error) {
	var status, payloadState string
	var version int64
	var releaseJob sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT status,version,payload_state,payload_release_job_id FROM games WHERE id=?
`, job.ScopeID).Scan(&status, &version, &payloadState, &releaseJob)
	if errors.Is(err, sql.ErrNoRows) {
		return false, releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if err != nil {
		return false, fmt.Errorf("payloadrelease/read game: %w", err)
	}
	if status != "DELETED" || !releaseJob.Valid || releaseJob.String != job.ID {
		return false, releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if version != job.Input.Inputs.ScopeVersion && payloadState != "FAILED" && payloadState != "RELEASED" {
		return false, releaseFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH")
	}
	if payloadState == "RELEASED" {
		return true, nil
	}
	if payloadState == "FAILED" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE games
SET payload_state='RELEASING',payload_last_error_code=NULL,
version=version+1,updated_at_ms=?
WHERE id=?
`, now, job.ScopeID); err != nil {
			return false, fmt.Errorf("payloadrelease/retry game: %w", err)
		}
	}
	return false, nil
}

func (service *Service) releaseGamePayload(ctx context.Context, transaction *sql.Tx, gameID string) error {
	now := service.now().UnixMilli()
	blobs, err := gameBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return err
	}
	sessions, err := gameConsumptionSessions(ctx, transaction, gameID)
	if err != nil {
		return err
	}
	if err := service.releaseGameSources(ctx, transaction, gameID, now); err != nil {
		return err
	}
	if err := releaseGameConsumptions(ctx, transaction, gameID, now); err != nil {
		return err
	}
	if err := stopGameRuntime(ctx, transaction, gameID, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE content_hash_evidence
SET blob_id=NULL,archive_blob_id=NULL,archive_entry_ordinal=NULL,payload_released_at_ms=?
WHERE payload_released_at_ms IS NULL AND scrape_run_id IN (
  SELECT id FROM metadata_scrape_runs WHERE game_id=?
)
`, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/release game evidence: %w", err)
	}
	for _, statement := range gameDeleteStatements() {
		if err := execBatches(ctx, transaction, statement, gameID); err != nil {
			return err
		}
	}
	purged, err := service.purgeEligibleUploads(ctx, transaction, sessions, now)
	if err != nil {
		return err
	}
	blobs = append(blobs, purged...)
	if err := assertGameReleased(ctx, transaction, gameID); err != nil {
		return err
	}
	if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE games SET payload_state='RELEASED',payload_released_at_ms=?,payload_last_error_code=NULL,
version=version+1,updated_at_ms=?
WHERE id=? AND status='DELETED' AND payload_state IN ('RELEASING','FAILED','RELEASED')
`, now, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/complete game: %w", err)
	}
	return nil
}

func (service *Service) waitForGameMutations(ctx context.Context, gameID string) error {
	for {
		var active int
		if err := service.database.QueryRowContext(ctx, `
SELECT count(*) FROM jobs WHERE scope_type='GAME' AND scope_id=?
AND kind IN ('GAME_CONTENT_REPLACE','METADATA_SCRAPE','MEDIA_FETCH')
AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, gameID).Scan(&active); err != nil {
			return fmt.Errorf("payloadrelease/check mutations: %w", err)
		}
		if active == 0 {
			return nil
		}
		if err := service.waitFor(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

func stopGameRuntime(ctx context.Context, transaction *sql.Tx, gameID string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1
WHERE game_id=? AND state IN ('CREATED','ACTIVE')
`, now, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/revoke launches: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE game_id=? AND state='ACTIVE'
`, now, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/end play sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason='GAME_DELETED',updated_at_ms=?,version=version+1
WHERE game_id=? AND state NOT IN ('FINISHED','FAILED')
`, now, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/end netplay sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='ENDED',ended_at_ms=?,end_reason='GAME_DELETED',updated_at_ms=?,version=version+1
WHERE selected_game_id=? AND state IN ('DRAFT','WAITING','STARTING','RUNNING')
`, now, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/end netplay rooms: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET save_state_id=NULL WHERE game_id=? AND save_state_id IS NOT NULL
`, gameID); err != nil {
		return fmt.Errorf("payloadrelease/unlink launch saves: %w", err)
	}
	return nil
}

func gameDeleteStatements() []string {
	return []string{
		`DELETE FROM save_states WHERE rowid IN (SELECT rowid FROM save_states WHERE game_id=? ORDER BY rowid LIMIT 200)`,
		`DELETE FROM launch_external_files WHERE rowid IN (
 SELECT file.rowid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM launch_content_files WHERE rowid IN (
 SELECT file.rowid FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM scrape_candidate_assets WHERE rowid IN (
 SELECT asset.rowid FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.game_id=? ORDER BY asset.rowid LIMIT 200
)`,
		`DELETE FROM game_assets WHERE rowid IN (SELECT rowid FROM game_assets WHERE game_id=? ORDER BY rowid LIMIT 200)`,
		`DELETE FROM game_files WHERE rowid IN (
 SELECT file.rowid FROM game_files file
 WHERE file.game_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM variant_files WHERE rowid IN (
 SELECT file.rowid FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id
 WHERE variant.game_id=? ORDER BY file.rowid LIMIT 200
)`,
	}
}

func releaseGameConsumptions(ctx context.Context, transaction *sql.Tx, gameID string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE upload_consumptions SET released_at_ms=?,release_reason='GAME_DELETED',version=version+1
WHERE released_at_ms IS NULL AND (
  consumer_type='GAME_ASSET' AND consumer_id IN (SELECT id FROM game_assets WHERE game_id=?) OR
	  consumer_type='GAME_CONTENT_REPLACE_JOB' AND consumer_id=(
	    SELECT content_source_ref_id FROM games WHERE id=? AND content_source_kind='ADMIN_REPLACE'
	  )
)
`, now, gameID, gameID); err != nil {
		return fmt.Errorf("payloadrelease/release game consumptions: %w", err)
	}
	return nil
}

func assertGameReleased(ctx context.Context, transaction *sql.Tx, gameID string) error {
	var count int
	err := transaction.QueryRowContext(ctx, `
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
`, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID).Scan(&count)
	if err != nil || count != 0 {
		return releaseFailure("PAYLOAD_RELEASE_REFERENCE_REMAINS")
	}
	return nil
}
