package gamerelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/payloadrelease"
)

func Owner(ctx context.Context, executor dbapi.Executor, scope application.Scope) (application.Owner, error) {
	return wrapPair(releaseops.ReadOwner(ctx, executor, scope,
		`SELECT status,version,payload_state,COALESCE(payload_release_job_id,''),'',0 FROM games WHERE id=?`))
}
