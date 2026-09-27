package itemrelease

import (
	"context"
	"database/sql"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

func Fail(ctx context.Context, executor dbapi.Executor, update recordstore.Update, scope application.ScopeType) (sql.Result, error) {
	if scope == application.ScopeImportItem {
		return recordstore.UpdateImportItems(ctx, executor, update)
	}
	args := append(append([]any{}, update.Values...), update.Scope.Args...)
	return executor.ExecContext(ctx, "UPDATE import_jobs SET "+update.Set+" WHERE "+update.Scope.Where, args...)
}
