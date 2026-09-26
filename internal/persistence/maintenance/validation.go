package maintenance

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/database/sqlite"

	"retrom/internal/cleanup"
	"retrom/internal/service/maintenance"
)

func openDatabase(ctx context.Context, path string) (dbapi.DB, error) {
	db, err := sqlite.Open(path, sqlite.Options{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return nil, fmt.Errorf("maintenance/bundle: %w", err)
	}
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			cleanup.Error("close", db.Close())
			return nil, fmt.Errorf("maintenance/bundle: %w", err)
		}
	}
	return db, nil
}

func checkDatabase(ctx context.Context, db dbapi.DB) error {
	var integrity string
	if err := dbapi.QueryRowContext(ctx, db, "PRAGMA integrity_check").Scan(&integrity); err != nil {
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
