package gamecontent

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/model/gamecontent"
)

func (writes writes) Fail(ctx context.Context, outcome gamecontent.Outcome) (bool, error) {
	state, expected := "FAILED", "RUNNING"
	var code *string
	if outcome.Cancelled {
		state, expected = "CANCELLED", "CANCEL_REQUESTED"
	} else {
		code = &outcome.Code
	}
	changed, err := changed(writes.transaction.ExecContext(ctx, `UPDATE jobs SET state=?,error_code=?,error_retryable=?,
 finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND state=? AND execution_no=? AND worker_id=?`, state, code, outcome.Retryable, outcome.Now, outcome.Now,
		outcome.JobID, expected, outcome.ExecutionNo, outcome.WorkerID))
	if err != nil || !changed {
		return changed, err
	}
	data := "{}"
	if !outcome.Cancelled {
		data = fmt.Sprintf(`{"code":%q}`, outcome.Code)
	}
	if err := writes.event(ctx, outcome.JobID, state, data, outcome.Now); err != nil {
		return false, err
	}
	return true, nil
}

func (writes writes) Succeed(ctx context.Context, outcome gamecontent.Outcome) error {
	err := requireChanged(
		writes.transaction.ExecContext(
			ctx,
			`UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,
 leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
 WHERE id=? AND state='RUNNING' AND execution_no=? AND worker_id=?`,
			outcome.Now,
			outcome.Now,
			outcome.JobID,
			outcome.ExecutionNo,
			outcome.WorkerID,
		),
	)
	if err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{
		"gameId": outcome.GameID, "gameVariantId": outcome.VariantID,
		"manifestDigest": outcome.ManifestDigest, "retiredSaveStateCount": outcome.RetiredSaveCount,
	})
	if err != nil {
		return fmt.Errorf("encode replacement success: %w", err)
	}
	return writes.event(ctx, outcome.JobID, "SUCCEEDED", string(data), outcome.Now)
}

func (writes writes) event(ctx context.Context, id, event, data string, now int64) error {
	_, err := writes.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 SELECT id,scope_type,scope_id,?,?,? FROM jobs WHERE id=?`,
		event,
		data,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("insert replacement event: %w", err)
	}
	return nil
}
