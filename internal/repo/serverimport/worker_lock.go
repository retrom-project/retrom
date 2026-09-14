package serverimport

import (
	"context"

	"retrom/internal/model/serverimport"
	"retrom/internal/repo/dbexec"
)

type WorkerAccess = serverimport.WorkerAccess

const (
	RunningWorker   = serverimport.RunningWorker
	CancelledWorker = serverimport.CancelledWorker
	ExhaustedWorker = serverimport.ExhaustedWorker
)

// LockWorker serializes import writes with lease claims across repositories.
func LockWorker(
	ctx context.Context,
	executor dbexec.Executor,
	unit serverimport.Work,
	now int64,
	access WorkerAccess,
) error {
	state := "RUNNING"
	if access == CancelledWorker {
		state = "CANCEL_REQUESTED"
	}
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET version=version+1,updated_at_ms=?
WHERE id=? AND kind='SERVER_BIOS_IMPORT' AND scope_type='SERVER_IMPORT' AND scope_id=?
AND execution_no=? AND worker_id=? AND worker_id<>'' AND state=?
AND EXISTS(SELECT 1 FROM server_imports import WHERE import.id=? AND import.job_id=jobs.id AND import.state=?)
AND ((?=0 AND leased_until_ms>?) OR ?=1 OR (?=2 AND attempt_count>=max_attempts AND leased_until_ms<=?))`,
		now, unit.JobID, unit.ImportID, unit.Execution, unit.Owner, state, unit.ImportID, state,
		access, now, access, access, now)
	return requireControlChange(result, err, serverimport.ErrLeaseLost)
}
