package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/service/metadatascrape"
)

func (records mediaRecords) Snapshot(ctx context.Context, id string) (metadatascrape.MediaSnapshot, error) {
	var snapshot metadatascrape.MediaSnapshot
	job := &snapshot.Job
	err := records.executor.QueryRowContext(ctx, `SELECT j.id,j.state,COALESCE(j.worker_id,''),
 j.scope_type,j.scope_id,j.execution_no,j.attempt_count,j.max_attempts,j.version,
 COALESCE(j.execution_deadline_at_ms,0),COALESCE(j.leased_until_ms,0),j.available_at_ms,
 COALESCE(i.input_json,''),COALESCE(i.input_digest,'') FROM jobs j LEFT JOIN job_input_snapshots i
 ON i.job_id=j.id AND i.execution_no=j.execution_no WHERE j.id=? AND j.kind='MEDIA_FETCH'`, id).
		Scan(&job.ID, &job.State, &job.WorkerID, &job.Scope.Kind, &job.Scope.ID, &job.Execution, &job.Attempt,
			&job.MaxAttempts, &job.Version, &job.Deadline, &job.LeaseUntil, &job.AvailableAt, &job.Input, &job.InputDigest)
	if err != nil {
		return snapshot, fmt.Errorf("query media job: %w", err)
	}
	asset := &snapshot.Asset
	err = records.executor.QueryRowContext(ctx, `SELECT a.id,a.scrape_candidate_id,a.provider_response_id,
 a.provider_asset_id,a.source_path,a.kind_hint,a.ordinal,a.media_fetch_job_id,a.status,a.version,
 COALESCE(a.media_fetch_order,-1),a.media_charged_bytes,a.media_reserved_bytes,c.scrape_run_id,
 CASE WHEN r.import_item_id IS NOT NULL THEN 'IMPORT_ITEM' ELSE 'GAME' END,
 COALESCE(r.import_item_id,r.game_id,''),COALESCE(i.state,g.status,''),COALESCE(i.payload_state,''),
 COALESCE(p.cancel_requested_at_ms IS NOT NULL,0),r.state,b.order_frozen_at_ms IS NOT NULL,
 b.charged_bytes,b.version,
 NOT EXISTS(SELECT 1 FROM scrape_candidate_assets earlier JOIN scrape_candidates ec ON ec.id=earlier.scrape_candidate_id
 JOIN jobs ej ON ej.id=earlier.media_fetch_job_id WHERE ec.scrape_run_id=r.id
 AND earlier.media_fetch_order<a.media_fetch_order AND ej.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))
 FROM scrape_candidate_assets a JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
 JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id JOIN metadata_media_runs b ON b.scrape_run_id=r.id
 LEFT JOIN import_items i ON i.id=r.import_item_id LEFT JOIN import_jobs p ON p.id=i.import_job_id
 LEFT JOIN games g ON g.id=r.game_id WHERE a.media_fetch_job_id=?`, id).
		Scan(&asset.ID, &asset.CandidateID, &asset.ResponseID, &asset.Reference.ProviderAssetID, &asset.Reference.Path,
			&asset.Reference.Kind, &asset.Reference.Ordinal, &asset.MediaJobID, &asset.Status, &asset.Version, &asset.Order,
			&asset.Charged, &asset.Reserved, &asset.RunID, &asset.OwnerKind, &asset.OwnerID,
			&asset.OwnerState, &asset.PayloadState,
			&asset.ParentCancelled, &snapshot.RunState, &snapshot.Frozen,
			&snapshot.Charged, &snapshot.RunVersion, &snapshot.First)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, fmt.Errorf("query media asset: %w", err)
	}
	return snapshot, nil
}

func (records mediaRecords) Running(ctx context.Context, now int64) (int, error) {
	var count int
	err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE kind='MEDIA_FETCH'
 AND state IN ('RUNNING','CANCEL_REQUESTED')
 AND leased_until_ms>? AND execution_deadline_at_ms>?`, now, now).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query active media executions: %w", err)
	}
	return count, nil
}

func (records mediaRecords) RunExecuting(ctx context.Context, id string, now int64) (bool, error) {
	var active bool
	err := records.executor.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs j
 JOIN scrape_candidate_assets a ON a.media_fetch_job_id=j.id
 JOIN scrape_candidates c ON c.id=a.scrape_candidate_id WHERE c.scrape_run_id=?
 AND j.state IN ('RUNNING','CANCEL_REQUESTED') AND j.leased_until_ms>? AND j.execution_deadline_at_ms>?)`,
		id, now, now).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("query active media run: %w", err)
	}
	return active, nil
}
