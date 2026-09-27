package sourcerelease

import (
	"context"
	"database/sql"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
)

func Fail(ctx context.Context, executor dbapi.Executor, update recordstore.Update) (sql.Result, error) {
	return recordstore.UpdateSourceImportItems(ctx, executor, update)
}
