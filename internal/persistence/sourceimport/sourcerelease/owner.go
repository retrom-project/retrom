package sourcerelease

import (
	"context"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/payloadrelease"
)

func Owner(ctx context.Context, executor dbapi.Executor, scope application.Scope) (application.Owner, error) {
	return releaseops.ReadOwner(ctx, executor, scope, `SELECT execution_state,version,payload_state,COALESCE(payload_release_job_id,''),
 COALESCE(library_import_item_id,''),retryable FROM source_import_items WHERE id=?`)
}
