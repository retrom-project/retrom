package gamerelease

import (
	"context"
	"database/sql"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

func Fail(ctx context.Context, executor dbapi.Executor, update recordstore.Update, change application.WorkChange) (sql.Result, error) {
	update.Set += ",version=version+1,updated_at_ms=?"
	update.Values = append(update.Values, change.NowMS)
	return recordstore.UpdateGames(ctx, executor, update)
}
