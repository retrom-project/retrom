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
	"retrom/internal/contentmanifest"
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
	errDatabaseFilename  = errors.New("invalid database filename")
	errMigrationFilename = errors.New("invalid migration name")
	errMigrationRebuild  = errors.New("unsupported migration foreign-key rebuild directive")
)

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

//nolint:gocyclo // Every read-only branch distinguishes a stable startup failure without touching the database.
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
	var migrationTableCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='schema_migrations'
`).Scan(&migrationTableCount); err != nil {
		return fmt.Errorf("%w: migration catalog", ErrSchemaInvalid)
	}
	if migrationTableCount == 0 {
		if tableCount == 0 {
			return nil
		}
		return fmt.Errorf("%w: migration catalog missing", ErrSchemaInvalid)
	}
	var count int
	var minimum, maximum sql.NullInt64
	if err := database.QueryRowContext(ctx, `
SELECT count(*),min(version),max(version) FROM schema_migrations
`).Scan(&count, &minimum, &maximum); err != nil {
		return fmt.Errorf("%w: migration catalog unreadable", ErrSchemaInvalid)
	}
	if count == 0 {
		if tableCount == 1 {
			return nil
		}
		return fmt.Errorf("%w: empty migration history with business tables", ErrSchemaInvalid)
	}
	latest, err := latestMigrationVersion()
	if err != nil {
		return err
	}
	if maximum.Int64 > int64(latest) {
		return fmt.Errorf("%w: %d", ErrFutureSchema, maximum.Int64)
	}
	if !minimum.Valid || !maximum.Valid || minimum.Int64 != 1 || maximum.Int64 != int64(count) {
		return fmt.Errorf("%w: migration history has gaps", ErrSchemaInvalid)
	}
	if maximum.Int64 < 23 {
		return fmt.Errorf("%w: found=%d required=23; use a new data root", ErrDatabaseRebuild, maximum.Int64)
	}
	return nil
}

func latestMigrationVersion() (int, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return 0, fmt.Errorf("read migrations: %w", err)
	}
	latest := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if parseErr != nil || version <= latest {
			return 0, fmt.Errorf("%w: %s", errMigrationFilename, entry.Name())
		}
		latest = version
	}
	return latest, nil
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
	foreignKeysDisabled := bytes.HasPrefix(contents, []byte("-- retrom: rebuild-with-foreign-keys-off\n"))
	if foreignKeysDisabled {
		if err := prepareForeignKeyRebuild(ctx, connection, version, name); err != nil {
			return err
		}
		defer func() {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON")
		}()
	}
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
	if foreignKeysDisabled {
		if err := verifyMigrationForeignKeys(ctx, connection); err != nil {
			return fmt.Errorf("verify migration %s foreign keys: %w", name, err)
		}
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

func prepareForeignKeyRebuild(ctx context.Context, connection *sql.Conn, version int, name string) error {
	if version != 19 && version != 24 && version != 26 && version != 28 && version != 30 && version != 37 {
		return fmt.Errorf("migration %s: %w", name, errMigrationRebuild)
	}
	if version == 19 {
		if err := verifyImportItemSourceManifests(ctx, connection); err != nil {
			return fmt.Errorf("preflight migration %s: %w", name, err)
		}
	}
	if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for migration %s: %w", name, err)
	}
	return nil
}

func verifyMigrationForeignKeys(ctx context.Context, connection *sql.Conn) error {
	rows, err := connection.QueryContext(ctx, "PRAGMA foreign_key_check")
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

type migrationImportItem struct {
	id       string
	manifest string
	digest   string
}

func verifyImportItemSourceManifests(ctx context.Context, connection *sql.Conn) error {
	items, err := readMigrationImportItems(ctx, connection)
	if err != nil {
		return err
	}
	for _, item := range items {
		files, loadErr := migrationManifestFiles(ctx, connection, item.id)
		if loadErr != nil {
			return loadErr
		}
		manifest, digest, buildErr := contentmanifest.Build(files)
		if buildErr != nil {
			return fmt.Errorf("build import source manifest %s: %w", item.id, buildErr)
		}
		if string(manifest) != item.manifest || digest != item.digest {
			return fmt.Errorf("import source manifest %s: %w", item.id, ErrMigrationChecksum)
		}
	}
	return nil
}

func readMigrationImportItems(ctx context.Context, connection *sql.Conn) ([]migrationImportItem, error) {
	rows, err := connection.QueryContext(ctx, `
SELECT id,source_manifest_json,source_manifest_digest
FROM import_items
ORDER BY id
`)
	if err != nil {
		return nil, fmt.Errorf("query import source manifests: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]migrationImportItem, 0)
	for rows.Next() {
		var item migrationImportItem
		if err := rows.Scan(&item.id, &item.manifest, &item.digest); err != nil {
			return nil, fmt.Errorf("scan import source manifest: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan import source manifests: %w", err)
	}
	return items, nil
}

func migrationManifestFiles(
	ctx context.Context,
	connection *sql.Conn,
	itemID string,
) ([]contentmanifest.File, error) {
	rows, err := connection.QueryContext(ctx, `
SELECT source.role,
source.logical_name,
blob.sha256,
blob.size_bytes,
archive.sha256,
source.source_archive_entry_ordinal
FROM import_item_source_files source
JOIN blobs blob ON blob.id=source.blob_id
LEFT JOIN blobs archive ON archive.id=source.source_archive_blob_id
WHERE source.import_item_id=?
ORDER BY source.role,source.logical_name
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query import source files %s: %w", itemID, err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]contentmanifest.File, 0)
	for rows.Next() {
		var file contentmanifest.File
		var archiveSHA sql.NullString
		var archiveOrdinal sql.NullInt64
		if err := rows.Scan(
			&file.Role,
			&file.LogicalName,
			&file.BlobSHA256,
			&file.SizeBytes,
			&archiveSHA,
			&archiveOrdinal,
		); err != nil {
			return nil, fmt.Errorf("scan import source file %s: %w", itemID, err)
		}
		if archiveSHA.Valid != archiveOrdinal.Valid {
			return nil, fmt.Errorf("import source archive %s: %w", itemID, contentmanifest.ErrInvalid)
		}
		if archiveSHA.Valid {
			ordinal := int(archiveOrdinal.Int64)
			file.SourceArchiveSHA256 = &archiveSHA.String
			file.SourceArchiveEntryOrdinal = &ordinal
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan import source files %s: %w", itemID, err)
	}
	return files, nil
}
