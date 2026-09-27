package testsupport

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
)

// ExecuteSeed gives SQL fixture batches the same explicit Blob write boundary
// as domain repositories. The fixture names each owning table directly.
func ExecuteSeed(ctx context.Context, db dbapi.Executor, table, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	var err error
	if table == "" {
		result, err = db.ExecContext(ctx, query, args...)
	} else {
		result, err = recordstore.InsertRows(ctx, db, table, query, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("seed fixture references: %w", err)
	}
	return result, nil
}
