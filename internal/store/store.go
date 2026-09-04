package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
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
	ErrDatabaseRebuild   = errors.New("DATABASE_REBUILD_REQUIRED")
	ErrSchemaInvalid     = errors.New("DATABASE_SCHEMA_INVALID")
	errIntegrityCheck    = errors.New("sqlite integrity check failed")
	errForeignKeyCheck   = errors.New("sqlite foreign key check failed")
	errForeignKeysOff    = errors.New("migration foreign keys remain disabled")
	errDatabaseFilename  = errors.New("invalid database filename")
	errMigrationFilename = errors.New("invalid migration name")
)

type migrationSource struct {
	version  int
	name     string
	checksum string
	contents []byte
}

type MigrationLineage struct {
	Version int64
	Digest  string
}

type DB struct {
	SQL      *sql.DB
	ReadOnly *sql.DB
}

func Open(ctx context.Context, path string, now func() time.Time) (*DB, error) {
	if err := preflightExistingDatabase(ctx, path); err != nil {
		return nil, err
	}
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
	readOnly, err := openReadOnlyDatabase(ctx, path)
	if err != nil {
		cleanup.Error("close", database.Close())
		return nil, err
	}
	return &DB{SQL: database, ReadOnly: readOnly}, nil
}

func openReadOnlyDatabase(ctx context.Context, path string) (*sql.DB, error) {
	query := url.Values{}
	query.Set("mode", "ro")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: query.Encode()}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open read-only sqlite: %w", err)
	}
	// Read traffic has a small independent pool so health probes and page reads
	// cannot queue behind the single serialized writer connection.
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	if err := database.PingContext(ctx); err != nil {
		cleanup.Error("close", database.Close())
		return nil, fmt.Errorf("open read-only sqlite: %w", err)
	}
	return database, nil
}

// Every read-only branch distinguishes a stable startup failure without touching the database.
func preflightExistingDatabase(ctx context.Context, path string) error {
	info, err := fsStat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		if info.Mode().IsRegular() && info.Size() == 0 {
			return nil
		}
		return errDatabaseFilename
	}
	uri := "file:" + filepath.ToSlash(path) + "?mode=ro&immutable=1"
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		return fmt.Errorf("probe sqlite: %w", err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	var tableCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'
`).Scan(&tableCount); err != nil {
		return fmt.Errorf("%w: unreadable schema", ErrSchemaInvalid)
	}
	return inspectMigrationHistory(ctx, database, tableCount)
}

func inspectMigrationHistory(ctx context.Context, database *sql.DB, tableCount int) error {
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

func migrationCatalogExists(ctx context.Context, database *sql.DB) (bool, error) {
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='schema_migrations'
`).Scan(&count); err != nil {
		return false, fmt.Errorf("%w: migration catalog", ErrSchemaInvalid)
	}
	return count == 1, nil
}

func migrationHistoryBounds(ctx context.Context, database *sql.DB) (int, sql.NullInt64, sql.NullInt64, error) {
	var count int
	var minimum, maximum sql.NullInt64
	if err := database.QueryRowContext(ctx, `
SELECT count(*),min(version),max(version) FROM schema_migrations
`).Scan(&count, &minimum, &maximum); err != nil {
		return 0, sql.NullInt64{}, sql.NullInt64{}, fmt.Errorf("%w: migration catalog unreadable", ErrSchemaInvalid)
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

func validateMigrationRecords(ctx context.Context, database *sql.DB, sources []migrationSource) error {
	rows, err := database.QueryContext(ctx, `
SELECT version,name,checksum FROM schema_migrations ORDER BY version
`)
	if err != nil {
		return fmt.Errorf("%w: migration catalog unreadable", ErrSchemaInvalid)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			return fmt.Errorf("%w: migration catalog unreadable", ErrSchemaInvalid)
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
		return fmt.Errorf("%w: migration catalog unreadable", ErrSchemaInvalid)
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

func CurrentMigrationLineage() (MigrationLineage, error) {
	sources, err := migrationSources()
	if err != nil {
		return MigrationLineage{}, err
	}
	digest := sha256.New()
	for _, source := range sources {
		_, _ = digest.Write([]byte(source.name))
		_, _ = digest.Write([]byte{'\x00'})
		_, _ = digest.Write([]byte(source.checksum))
		_, _ = digest.Write([]byte{'\n'})
	}
	return MigrationLineage{
		Version: int64(len(sources)),
		Digest:  hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func ValidateCurrentMigrationLineage(ctx context.Context, database *sql.DB) (MigrationLineage, error) {
	lineage, err := CurrentMigrationLineage()
	if err != nil {
		return MigrationLineage{}, err
	}
	var count int64
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		return MigrationLineage{}, fmt.Errorf("%w: migration catalog unreadable", ErrSchemaInvalid)
	}
	if count != lineage.Version {
		return MigrationLineage{}, fmt.Errorf("%w: incomplete migration lineage", ErrSchemaInvalid)
	}
	if err := inspectMigrationHistory(ctx, database, 2); err != nil {
		return MigrationLineage{}, err
	}
	return lineage, nil
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

var (
	fsMkdirAll = mkdirAll
	fsStat     = os.Stat
)

// Contract branches stay contiguous for a single auditable decision.
func applyMigrations(ctx context.Context, database *sql.DB, now func() time.Time) error {
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
	if err := database.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maximum); err != nil {
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
	database *sql.DB,
	source migrationSource,
	now func() time.Time,
) error {
	var existingName, existingChecksum string
	err := database.QueryRowContext(ctx,
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
	database *sql.DB,
	source migrationSource,
	now func() time.Time,
) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get migration connection: %w", err)
	}
	defer func() { cleanup.Error("close", connection.Close()) }()
	foreignKeysDisabled := bytes.HasPrefix(source.contents, []byte("-- retrom:foreign-keys-off\n"))
	if foreignKeysDisabled {
		if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return fmt.Errorf("disable migration foreign keys: %w", err)
		}
		defer func() {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON")
		}()
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin migration %s: %w", source.name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if _, err := connection.ExecContext(ctx, string(source.contents)); err != nil {
		return fmt.Errorf("apply migration %s: %w", source.name, err)
	}
	if foreignKeysDisabled {
		if err := verifyMigrationForeignKeys(ctx, connection); err != nil {
			return fmt.Errorf("verify migration %s foreign keys: %w", source.name, err)
		}
	}
	if _, err := connection.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, name, checksum, applied_at_ms) VALUES(?,?,?,?)",
		source.version, source.name, source.checksum, now().UTC().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %s: %w", source.name, err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration %s: %w", source.name, err)
	}
	committed = true
	if foreignKeysDisabled {
		if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
			return fmt.Errorf("restore migration foreign keys: %w", err)
		}
		var enabled int
		if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			return fmt.Errorf("read restored migration foreign keys: %w", err)
		}
		if enabled != 1 {
			return fmt.Errorf("restore migration foreign keys: %w: enabled=%d", errForeignKeysOff, enabled)
		}
	}
	return nil
}

type foreignKeyQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func verifyMigrationForeignKeys(ctx context.Context, database foreignKeyQuerier) error {
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("query foreign key check: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	if rows.Next() {
		return errForeignKeyCheck
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan foreign key check: %w", err)
	}
	return nil
}
