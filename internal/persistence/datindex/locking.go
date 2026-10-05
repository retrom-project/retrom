package datindex

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
)

// LockCatalogWrites must be the first operation in a catalog write transaction.
// Catalog publishers share one database-local lock; game and account writers do
// not acquire it. READ COMMITTED sees a previous publisher after waiting and
// avoids SSI predicate conflicts while materializing large immutable catalogs.
func LockCatalogWrites(ctx context.Context, transaction dbapi.Tx) error {
	if _, err := transaction.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"); err != nil {
		return fmt.Errorf("configure catalog publication: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "SET LOCAL lock_timeout = '60s'"); err != nil {
		return fmt.Errorf("configure catalog lock wait: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(824392572)"); err != nil {
		return fmt.Errorf("lock catalog publication: %w", err)
	}
	return nil
}
