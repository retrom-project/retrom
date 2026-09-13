package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/service/metadatascrape"
)

func (records workerRecords) Finish(ctx context.Context, value metadatascrape.WorkerOutcome) error {
	claim := value.Claim
	if claim.WorkerID != "" {
		var code *string
		var retryable *bool
		if value.State == "FAILED" {
			code = &value.Code
			retry := true
			retryable = &retry
		}
		changed, err := workerChanged(
			records.transaction.ExecContext(
				ctx,
				`UPDATE jobs SET state=?,error_code=?,error_retryable=?,
 finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
 WHERE id=? AND execution_no=? AND worker_id=? AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')`,

				value.State,
				code,
				retryable,
				value.Now,
				value.Now,
				value.Now,
				claim.JobID,
				claim.ExecutionNo,
				claim.WorkerID,
			),
		)
		if err != nil {
			return err
		}
		if !changed {
			return metadatascrape.ErrExecutionLost
		}
	}
	var code *string
	if value.Code != "" {
		code = &value.Code
	}
	result, err := records.transaction.ExecContext(
		ctx,
		`UPDATE metadata_scrape_runs SET state=?,error_code=?,version=version+1,
 updated_at_ms=?,completed_at_ms=? WHERE id=? AND job_id=? AND state='RUNNING'`,
		value.RunState,
		code,
		value.Now,
		value.Now,
		claim.RunID,
		claim.JobID,
	)
	if err := scheduleChanged(result, err, metadatascrape.ErrExecutionLost); err != nil {
		return err
	}
	if claim.WorkerID == "" {
		return nil
	}
	data := fmt.Sprintf(`{"candidateCount":%d}`, value.Count)
	if value.Code != "" {
		data = fmt.Sprintf(`{"code":%q}`, value.Code)
	}
	return records.event(ctx, claim.JobID, value.State, data, value.Now)
}
