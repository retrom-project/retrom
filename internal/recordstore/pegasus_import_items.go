package recordstore

import (
	"context"
	"database/sql"
)

func CreatePegasusImportItems(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidatePegasusImportItems)
}

func ValidatePegasusImportItems(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, pegasus_import_itemsOwnership, keys)
}

const pegasus_import_itemsOwnership = `
SELECT CASE
WHEN (candidate.library_import_item_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM emulationstation_import_items WHERE
library_import_item_id=candidate.library_import_item_id
)) THEN 'server source review already owned'
ELSE '' END
FROM pegasus_import_items candidate
WHERE candidate.id=?`
