package firmware

import (
	"context"

	"retrom/internal/model/firmware"
)

func (store writes) LockExecution(ctx context.Context, value firmware.ServerExecution) error {
	return changed(store.transaction.ExecContext(ctx, `UPDATE jobs SET version=version+1,updated_at_ms=?
WHERE id=? AND kind='SERVER_BIOS_IMPORT' AND scope_type='SERVER_IMPORT' AND scope_id=?
AND execution_no=? AND worker_id=? AND worker_id<>'' AND state='RUNNING' AND leased_until_ms>?
AND EXISTS(SELECT 1 FROM server_imports import WHERE import.id=? AND import.job_id=jobs.id AND import.state='RUNNING')`,
		value.AtMS, value.JobID, value.ImportID, value.ExecutionNo, value.WorkerID, value.AtMS, value.ImportID))
}
