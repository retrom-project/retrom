package cleanupjobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/filestore"
)

func scratchMaintenance(files *filestore.Store, database dbapi.DB, now func() time.Time) func(context.Context) error {
	var mutex sync.Mutex
	var next time.Time
	return func(ctx context.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		current := now()
		if current.Before(next) {
			return nil
		}
		if err := files.SweepWrites(ctx, current.Add(-filestore.ScratchLifetime)); err != nil {
			return fmt.Errorf("scratch maintenance: %w", err)
		}
		if err := files.SweepUncommitted(ctx, current.Add(-filestore.ScratchLifetime),
			func(ctx context.Context, kind, id string) (bool, error) {
				queries := map[string]string{
					"items":   "SELECT EXISTS(SELECT 1 FROM import_items WHERE id=?)",
					"uploads": "SELECT EXISTS(SELECT 1 FROM upload_sessions WHERE id=?)",
					"sources": "SELECT EXISTS(SELECT 1 FROM source_import_items WHERE id=?)",
				}
				var active bool
				if err := dbapi.QueryRowContext(ctx, database, queries[kind], id).Scan(&active); err != nil {
					return false, fmt.Errorf("read staging owner: %w", err)
				}
				return active, nil
			}); err != nil {
			return fmt.Errorf("sweep uncommitted directories: %w", err)
		}
		next = current.Add(time.Hour)
		return nil
	}
}
