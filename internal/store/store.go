package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Register the modernc SQLite driver used by Open.

	"retrom/internal/cleanup"
	"retrom/migrations"
)

const migrationTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  checksum TEXT NOT NULL CHECK(length(checksum) = 64),
  applied_at_ms INTEGER NOT NULL CHECK(applied_at_ms >= 0)
)
`

var (
	ErrMigrationChecksum = errors.New("MIGRATION_CHECKSUM_MISMATCH")
	ErrFutureSchema      = errors.New("DATABASE_SCHEMA_TOO_NEW")
	errIntegrityCheck    = errors.New("sqlite integrity check failed")
	errForeignKeyCheck   = errors.New("sqlite foreign key check failed")
	errDatabaseFilename  = errors.New("invalid database filename")
	errMigrationFilename = errors.New("invalid migration name")
)

type DB struct {
	SQL *sql.DB
}

func Open(ctx context.Context, path string, now func() time.Time) (*DB, error) {
	if err := ensureParent(path); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Retrom has one write handle. Serializing it prevents per-connection PRAGMA drift
	// and follows the documented single-writer SQLite contract.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := database.ExecContext(ctx, pragma); err != nil {
			cleanup.Error("close", database.Close())
			return nil, fmt.Errorf("configure sqlite: %w", err)
		}
	}
	if err := applyMigrations(ctx, database, now); err != nil {
		cleanup.Error("close", database.Close())
		return nil, err
	}
	return &DB{SQL: database}, nil
}

func (database *DB) Close() error {
	if err := database.SQL.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

func (database *DB) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := database.SQL.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("sqlite integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("%w: %s", errIntegrityCheck, result)
	}
	rows, err := database.SQL.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("sqlite foreign key check: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	if rows.Next() {
		return errForeignKeyCheck
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate foreign key check: %w", err)
	}
	return nil
}

func ensureParent(path string) error {
	parent := filepath.Dir(path)
	if err := fs.ValidPath(filepath.ToSlash(filepath.Base(path))); !err {
		return errDatabaseFilename
	}
	if err := mkdirAllOwnerOnly(parent); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}

func mkdirAllOwnerOnly(path string) error {
	// The data-root validator rejects symlink roots. MkdirAll only creates missing descendants.
	return fsMkdirAll(path, 0o700)
}

var fsMkdirAll = mkdirAll

//nolint:gocyclo // Contract branches stay contiguous for a single auditable decision.
func applyMigrations(ctx context.Context, database *sql.DB, now func() time.Time) error {
	if _, err := database.ExecContext(ctx, migrationTable); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	latest := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if parseErr != nil || version <= latest {
			return fmt.Errorf("%w: %s", errMigrationFilename, entry.Name())
		}
		latest = version
		contents, readErr := migrations.Files.ReadFile(entry.Name())
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), readErr)
		}
		checksumBytes := sha256.Sum256(contents)
		checksum := hex.EncodeToString(checksumBytes[:])
		var existing string
		scanErr := database.QueryRowContext(ctx,
			"SELECT checksum FROM schema_migrations WHERE version = ?", version).Scan(&existing)
		switch {
		case scanErr == nil:
			if existing != checksum {
				return fmt.Errorf("%w: %03d", ErrMigrationChecksum, version)
			}
			continue
		case !errors.Is(scanErr, sql.ErrNoRows):
			return fmt.Errorf("read migration record: %w", scanErr)
		}
		if err := runMigration(ctx, database, version, entry.Name(), checksum, contents, now); err != nil {
			return err
		}
	}
	var maximum sql.NullInt64
	if err := database.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maximum); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if maximum.Valid && maximum.Int64 > int64(latest) {
		return fmt.Errorf("%w: %d", ErrFutureSchema, maximum.Int64)
	}
	return nil
}

func runMigration(
	ctx context.Context,
	database *sql.DB,
	version int,
	name string,
	checksum string,
	contents []byte,
	now func() time.Time,
) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get migration connection: %w", err)
	}
	defer func() { cleanup.Error("close", connection.Close()) }()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if _, err := connection.ExecContext(ctx, string(contents)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := connection.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, name, checksum, applied_at_ms) VALUES(?,?,?,?)",
		version, name, checksum, now().UTC().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	committed = true
	return nil
}
