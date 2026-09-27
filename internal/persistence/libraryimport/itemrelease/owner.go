package itemrelease

import (
	"context"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/payloadrelease"
)

func Owner(ctx context.Context, executor dbapi.Executor, scope application.Scope) (application.Owner, error) {
	query := `SELECT state,version,payload_state,COALESCE(payload_release_job_id,''),'',0 FROM import_items WHERE id=?`
	if scope.Type == application.ScopeImportJob {
		query = `SELECT state,version,payload_state,COALESCE(payload_release_job_id,''),'',0 FROM import_jobs WHERE id=?`
	}
	return releaseops.ReadOwner(ctx, executor, scope, query)
}
func PendingChildren(ctx context.Context, executor dbapi.Executor, id string) (int64, error) {
	return (releaseops.Records{Executor: executor}).ReadCount(ctx, `SELECT count(*) FROM import_items WHERE import_job_id=?
 AND state NOT IN ('PUBLISHED','DISCARDED','FAILED_FINAL','CANCELLED')`, id)
}
