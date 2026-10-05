package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/database/postgres"
	"retrom/migrations"
)

const migrationTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version BIGINT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  checksum TEXT NOT NULL CHECK(length(checksum) = 64),
  applied_at_ms BIGINT NOT NULL CHECK(applied_at_ms >= 0)
)
`

var (
	ErrMigrationChecksum = errors.New("MIGRATION_CHECKSUM_MISMATCH")
	ErrFutureSchema      = errors.New("DATABASE_SCHEMA_TOO_NEW")
	ErrDatabaseRebuild   = errors.New("DATABASE_REBUILD_REQUIRED")
	ErrSchemaInvalid     = errors.New("DATABASE_SCHEMA_INVALID")
	errForeignKeyCheck   = errors.New("PostgreSQL foreign key check failed")
	errMigrationFilename = errors.New("invalid migration name")
)

type migrationSource struct {
	version  int
	name     string
	checksum string
	contents []byte
}

type DB struct {
	SQL      dbapi.DB
	ReadOnly dbapi.DB
}

func Open(ctx context.Context, dsn string, now func() time.Time) (*DB, error) {
	database, err := postgres.Open(dsn, postgres.Options{MaxOpenConns: 16, MaxIdleConns: 4, Now: now})
	if err != nil {
		return nil, fmt.Errorf("open database writer: %w", err)
	}
	if err := initializeSchema(ctx, database, now); err != nil {
		cleanup.Error("close", database.Close())
		return nil, err
	}
	reader, err := postgres.Open(dsn, postgres.Options{MaxOpenConns: 8, MaxIdleConns: 4, ReadOnly: true, Now: now})
	if err != nil {
		cleanup.Error("close", database.Close())
		return nil, fmt.Errorf("open database reader: %w", err)
	}
	if err := reader.PingContext(ctx); err != nil {
		cleanup.Error("close reader", reader.Close())
		cleanup.Error("close writer", database.Close())
		return nil, fmt.Errorf("ping database reader: %w", err)
	}
	return &DB{SQL: database, ReadOnly: reader}, nil
}

func initializeSchema(ctx context.Context, database dbapi.DB, now func() time.Time) error {
	err := dbapi.InTransaction(ctx, database, nil, func(tx dbapi.Tx) error {
		// The lock serializes schema changes. Each statement must see the
		// preceding initializer's commit after waiting for that lock.
		if _, err := tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"); err != nil {
			return fmt.Errorf("configure schema initialization: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(824392571)"); err != nil {
			return fmt.Errorf("lock schema initialization: %w", err)
		}
		var tableCount int
		if err := dbapi.QueryRowContext(ctx, tx, `
SELECT count(*) FROM pg_tables WHERE schemaname=current_schema()
`).Scan(&tableCount); err != nil {
			return fmt.Errorf("read database catalog: %w", err)
		}
		if err := inspectMigrationHistory(ctx, tx, tableCount); err != nil {
			return err
		}
		return applyMigrations(ctx, tx, now)
	})
	if err != nil {
		return fmt.Errorf("initialize schema: %w", err)
	}
	return nil
}

func inspectMigrationHistory(ctx context.Context, database dbapi.Executor, tableCount int) error {
	migrationCatalogExists, err := migrationCatalogExists(ctx, database)
	if err != nil {
		return err
	}
	if !migrationCatalogExists {
		if tableCount == 0 {
			return nil
		}
		return fmt.Errorf("%w: migration catalog missing", ErrSchemaInvalid)
	}
	count, minimum, maximum, err := migrationHistoryBounds(ctx, database)
	if err != nil {
		return err
	}
	if count == 0 {
		if tableCount == 1 {
			return nil
		}
		return fmt.Errorf("%w: empty migration history with business tables", ErrSchemaInvalid)
	}
	sources, err := migrationSources()
	if err != nil {
		return err
	}
	if err := validateMigrationHistoryBounds(count, minimum, maximum, len(sources)); err != nil {
		return err
	}
	return validateMigrationRecords(ctx, database, sources)
}

func migrationCatalogExists(ctx context.Context, database dbapi.Executor) (bool, error) {
	var count int
	if err := dbapi.QueryRowContext(ctx, database, `
SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename='schema_migrations'
`).Scan(&count); err != nil {
		return false, fmt.Errorf("%w: migration catalog: %w", ErrSchemaInvalid, err)
	}
	return count == 1, nil
}

func migrationHistoryBounds(ctx context.Context, database dbapi.Executor) (int, sql.NullInt64, sql.NullInt64, error) {
	var count int
	var minimum, maximum sql.NullInt64
	if err := dbapi.QueryRowContext(ctx, database, `
SELECT count(*),min(version),max(version) FROM schema_migrations
`).Scan(&count, &minimum, &maximum); err != nil {
		return 0, sql.NullInt64{}, sql.NullInt64{}, fmt.Errorf("%w: migration catalog unreadable: %w", ErrSchemaInvalid, err)
	}
	return count, minimum, maximum, nil
}

func validateMigrationHistoryBounds(count int, minimum, maximum sql.NullInt64, sourceCount int) error {
	if maximum.Int64 > int64(sourceCount) {
		return fmt.Errorf("%w: %d", ErrFutureSchema, maximum.Int64)
	}
	if !minimum.Valid || !maximum.Valid || minimum.Int64 != 1 || maximum.Int64 != int64(count) {
		return fmt.Errorf("%w: migration history has gaps", ErrSchemaInvalid)
	}
	return nil
}

func validateMigrationRecords(ctx context.Context, database dbapi.Executor, sources []migrationSource) error {
	rows, err := database.QueryContext(ctx, `
SELECT version,name,checksum FROM schema_migrations ORDER BY version
`)
	if err != nil {
		return fmt.Errorf("%w: migration catalog unreadable: %w", ErrSchemaInvalid, err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			return fmt.Errorf("%w: migration catalog unreadable: %w", ErrSchemaInvalid, err)
		}
		if version < 1 || version > len(sources) {
			return fmt.Errorf("%w: invalid migration version %d", ErrSchemaInvalid, version)
		}
		expected := sources[version-1]
		if name != expected.name {
			return fmt.Errorf("%w: migration %03d is from another lineage", ErrDatabaseRebuild, version)
		}
		if checksum != expected.checksum {
			return fmt.Errorf("%w: %03d", ErrMigrationChecksum, version)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: migration catalog unreadable: %w", ErrSchemaInvalid, err)
	}
	return nil
}

func migrationSources() ([]migrationSource, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	sources := make([]migrationSource, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if parseErr != nil || version != len(sources)+1 {
			return nil, fmt.Errorf("%w: %s", errMigrationFilename, entry.Name())
		}
		contents, readErr := migrations.Files.ReadFile(entry.Name())
		if readErr != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), readErr)
		}
		checksumBytes := sha256.Sum256(contents)
		sources = append(sources, migrationSource{
			version: version, name: entry.Name(), contents: contents,
			checksum: hex.EncodeToString(checksumBytes[:]),
		})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: no migrations", errMigrationFilename)
	}
	return sources, nil
}

func (database *DB) Close() error {
	var closeErrors []error
	if database.ReadOnly != nil {
		if err := database.ReadOnly.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close read-only database: %w", err))
		}
	}
	if database.SQL != nil {
		if err := database.SQL.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close database: %w", err))
		}
	}
	if err := errors.Join(closeErrors...); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// IntegrityCheck verifies the current schema lineage and validated relational
// constraints. Physical server maintenance belongs to PostgreSQL operations.
func (database *DB) IntegrityCheck(ctx context.Context) error {
	sources, err := migrationSources()
	if err != nil {
		return err
	}
	if err := validateMigrationRecords(ctx, database.ReadOnly, sources); err != nil {
		return err
	}
	return verifyMigrationForeignKeys(ctx, database.ReadOnly)
}

// Contract branches stay contiguous for a single auditable decision.
func applyMigrations(ctx context.Context, database dbapi.Executor, now func() time.Time) error {
	if _, err := database.ExecContext(ctx, migrationTable); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	sources, err := migrationSources()
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := applyMigration(ctx, database, source, now); err != nil {
			return err
		}
	}
	var maximum sql.NullInt64
	if err := dbapi.QueryRowContext(
		ctx, database, "SELECT max(version) FROM schema_migrations").Scan(&maximum); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if maximum.Valid && maximum.Int64 > int64(len(sources)) {
		return fmt.Errorf("%w: %d", ErrFutureSchema, maximum.Int64)
	}
	if err := verifyMigrationForeignKeys(ctx, database); err != nil {
		return fmt.Errorf("verify migrated schema foreign keys: %w", err)
	}
	return nil
}

func applyMigration(
	ctx context.Context,
	database dbapi.Executor,
	source migrationSource,
	now func() time.Time,
) error {
	var existingName, existingChecksum string
	err := dbapi.QueryRowContext(ctx, database,
		"SELECT name,checksum FROM schema_migrations WHERE version = ?", source.version).
		Scan(&existingName, &existingChecksum)
	if err == nil {
		if existingName != source.name {
			return fmt.Errorf("%w: migration %03d is from another lineage", ErrDatabaseRebuild, source.version)
		}
		if existingChecksum != source.checksum {
			return fmt.Errorf("%w: %03d", ErrMigrationChecksum, source.version)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read migration record: %w", err)
	}
	if err := runMigration(ctx, database, source, now); err != nil {
		return err
	}
	return nil
}

func runMigration(
	ctx context.Context,
	database dbapi.Executor,
	source migrationSource,
	now func() time.Time,
) error {
	if _, err := database.ExecContext(ctx, string(source.contents)); err != nil {
		return fmt.Errorf("apply migration %s: %w", source.name, err)
	}
	if _, err := database.ExecContext(ctx,
		"INSERT INTO schema_migrations(version,name,checksum,applied_at_ms) VALUES(?,?,?,?)",
		source.version, source.name, source.checksum, now().UTC().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %s: %w", source.name, err)
	}
	return nil
}

func verifyMigrationForeignKeys(ctx context.Context, database dbapi.Queryer) error {
	var invalid int
	err := dbapi.QueryRowContext(ctx, database, `
 SELECT (SELECT count(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace
 WHERE n.nspname=current_schema() AND NOT c.convalidated) +
 (SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
 JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND NOT i.indisvalid)
 `).Scan(&invalid)
	if err != nil {
		return fmt.Errorf("validate PostgreSQL schema: %w", err)
	}
	if invalid != 0 {
		return errForeignKeyCheck
	}
	return nil
}
