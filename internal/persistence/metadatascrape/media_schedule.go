package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/service/metadatascrape"
)

func (records resultRecords) Subject(ctx context.Context, runID string) (metadatascrape.Subject, error) {
	var scope metadatascrape.Subject
	err := records.transaction.QueryRowContext(ctx, `SELECT j.scope_type,j.scope_id FROM metadata_scrape_runs r
 JOIN jobs j ON j.id=r.job_id WHERE r.id=? AND
 ((j.scope_type='GAME' AND j.scope_id=r.game_id) OR
 (j.scope_type='IMPORT_ITEM' AND j.scope_id=r.import_item_id))`, runID).Scan(&scope.Kind, &scope.ID)
	if err != nil {
		return scope, fmt.Errorf("query media owner: %w", err)
	}
	return scope, nil
}

func (records resultRecords) Enqueue(ctx context.Context, plan metadatascrape.MediaJobPlan) error {
	_, err := records.transaction.ExecContext(ctx, `
INSERT INTO metadata_media_runs(scrape_run_id,created_at_ms,updated_at_ms)
 VALUES(?,?,?) ON CONFLICT(scrape_run_id) DO NOTHING`, plan.RunID, plan.Now, plan.Now)
	if err != nil {
		return fmt.Errorf("create media run budget: %w", err)
	}
	_, err = records.transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
 payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,?,?,'MEDIA_FETCH',?,1,'{"schemaVersion":1,"inputExecutionNo":1}',1,'QUEUED',0,4,?,?,?)`,
		plan.JobID, plan.Scope.Kind, plan.Scope.ID, plan.Dedupe, plan.Now, plan.Now, plan.Now)
	if err != nil {
		return fmt.Errorf("create media fetch job: %w", err)
	}
	_, err = records.transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
 VALUES(?,1,?,?,?)`, plan.JobID, plan.InputJSON, plan.InputDigest, plan.Now)
	if err != nil {
		return fmt.Errorf("freeze media input: %w", err)
	}
	_, err = records.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 VALUES(?,?,?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)`,
		plan.JobID, plan.Scope.Kind, plan.Scope.ID, plan.Now)
	if err != nil {
		return fmt.Errorf("record queued media event: %w", err)
	}
	return nil
}
