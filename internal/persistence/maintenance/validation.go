package maintenance

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/maintenance"

	// Register the driver used only by this database adapter.
	_ "modernc.org/sqlite"
)

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("maintenance/bundle: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			cleanup.Error("close", db.Close())
			return nil, fmt.Errorf("maintenance/bundle: %w", err)
		}
	}
	return db, nil
}

func checkDatabase(ctx context.Context, db *sql.DB) error {
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("check database integrity: %w", err)
	}
	if integrity != "ok" {
		return maintenance.ErrInvalidBundle
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	if rows.Next() {
		return maintenance.ErrInvalidBundle
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan foreign-key violations: %w", err)
	}
	return nil
}
