// Command seed-failed-deletion creates one persisted DeletionQueue failure in an isolated browser
// acceptance database. This is a fixture for the administrator retry action;
// normal automatic collection is verified separately before this fixture runs.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/database/sqlite"
	"retrom/internal/filestore"
	"retrom/internal/persistence/filecatalog"
	"retrom/internal/persistence/filedeletion"
	application "retrom/internal/service/cleanupjobs"
)

func main() {
	if err := seedFailedDeletion(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func seedFailedDeletion(ctx context.Context) error {
	path := os.Getenv("RETROM_E2E_DATABASE")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || !filepath.IsAbs(path) {
		return fmt.Errorf("acceptance database must be an existing absolute regular file: %w", os.ErrInvalid)
	}
	database, err := sqlite.Open(path, sqlite.Options{MaxOpenConns: 1})
	if err != nil {
		return err
	}
	defer func() { cleanup.Error("close acceptance database", database.Close()) }()
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout=30000"); err != nil {
		return err
	}
	store, err := filestore.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	metadata, err := store.Put(strings.NewReader("Retrom storage cleanup retry fixture v1"))
	if err != nil {
		return err
	}
	scheduler, err := application.NewDeletionScheduler(nil, application.DeletionOptions{})
	if err != nil {
		return err
	}
	return dbapi.InTransaction(ctx, database, nil, func(tx dbapi.Tx) error {
		now := time.Now().UnixMilli()
		id, err := filecatalog.EnsureRecord(ctx, tx, metadata, "application/octet-stream", now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE stored_files SET retired_at_ms=? WHERE id=?`, now, id); err != nil {
			return err
		}
		if err := scheduler.StageInScope(ctx, filedeletion.BindQueue(tx, application.WorkerScope{}), []string{id}); err != nil {
			return err
		}
		now = time.Now().UnixMilli()
		// Publish the candidate and its failed state together, so the live worker
		// never races this fixture. Real scheduling owns the input snapshot.
		result, err := tx.ExecContext(ctx, `UPDATE jobs SET state='FAILED',
 attempt_count=max_attempts,finished_at_ms=?,updated_at_ms=?,error_code='FILE_DELETE_IO_FAILED',error_retryable=1
 WHERE id=(SELECT deletion_job_id FROM file_deletions WHERE blob_id=?) AND state='QUEUED'`, now, now, id)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("expected one new failed DeletionQueue candidate: %w", os.ErrInvalid)
		}
		return nil
	})
}
