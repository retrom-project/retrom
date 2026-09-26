package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func CreateSourceImportItems(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateSourceImportItems)
}

func ValidateSourceImportItems(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, source_import_itemsOwnership, keys)
}

const source_import_itemsOwnership = `
SELECT ''
FROM source_import_items candidate
WHERE candidate.id=?`
