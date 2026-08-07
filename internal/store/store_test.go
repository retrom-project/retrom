package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/migrations"
)

func TestMigrationsCreateIntegerBusinessTimesAndSeedCatalog(t *testing.T) {
	t.Parallel()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1786000000000)
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	if err := database.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("IntegrityCheck() error = %v", err)
	}

	rows, err := database.SQL.QueryContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		if !strings.HasPrefix(name, "sqlite_") {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	for _, table := range tables {
		assertIntegerTimeColumns(t, database.SQL, table)
	}
	var platformCount, coreCount, directoryCount int
	if err := database.SQL.QueryRow("SELECT (SELECT COUNT(*) FROM platforms), (SELECT COUNT(*) FROM cores), (SELECT COUNT(*) FROM platform_instances)").Scan(
		&platformCount,
		&coreCount,
		&directoryCount,
	); err != nil {
		t.Fatalf("count seed: %v", err)
	}
	if platformCount != 7 || coreCount != 8 || directoryCount != 9 {
		t.Fatalf("seed counts = %d/%d/%d", platformCount, coreCount, directoryCount)
	}
}

func TestSupportedMigrationVersionsIdempotencyAndFutureProtection(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "supported_versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var supported []int
	if err := json.Unmarshal(contents, &supported); err != nil || len(supported) != 1 || supported[0] != 10 {
		t.Fatalf("supported versions = %#v, error=%v", supported, err)
	}
	fixtures, err := filepath.Glob(filepath.Join(repositoryRoot, "migrations", "testdata", "*_fixture.sql"))
	if err != nil || len(fixtures) != len(supported) || filepath.Base(fixtures[0]) != "010_fixture.sql" {
		t.Fatalf("migration fixtures = %#v, error=%v", fixtures, err)
	}
	path := filepath.Join(t.TempDir(), "retrom.db")
	database, err := Open(context.Background(), path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(context.Background(), path, time.Now)
	if err != nil {
		t.Fatalf("second current-schema open: %v", err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO schema_migrations(version,
name,
checksum,
applied_at_ms) VALUES(999,
'future.sql',
?,
?)
`, strings.Repeat("0", 64), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if future, err := Open(context.Background(), path, time.Now); !errors.Is(err, ErrFutureSchema) {
		if future != nil {
			cleanup.Error("close", future.Close())
		}
		t.Fatalf("future schema error = %v", err)
	}
}

func TestDOSExternalConfigMigrationRepointsLegacyLaunchContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(repositoryRoot, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if parseErr != nil || version > 10 {
			continue
		}
		migration, readErr := migrations.Files.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		digest := sha256.Sum256(migration)
		if err := runMigration(ctx, legacy, version, entry.Name(), fmt.Sprintf("%x", digest), migration, time.Now); err != nil {
			t.Fatal(err)
		}
	}
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "010_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", upgraded.Close()) })
	var logicalName, blobID, formatVersion string
	if err := upgraded.SQL.QueryRowContext(ctx, `
SELECT logical_name, blob_id, format_version
FROM launch_content_files
WHERE launch_session_id='dos-launch'
`).Scan(&logicalName, &blobID, &formatVersion); err != nil ||
		logicalName != "game.zip" || blobID != "base-blob" || formatVersion != "SOURCE_V1" {
		t.Fatalf("upgraded launch content = %s/%s/%s, error=%v", logicalName, blobID, formatVersion, err)
	}
	if _, err := upgraded.SQL.ExecContext(ctx, `UPDATE launch_content_files SET logical_name='changed.zip' WHERE launch_session_id='dos-launch'`); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("launch content immutability after migration error = %v", err)
	}
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}

func assertIntegerTimeColumns(t *testing.T, database *sql.DB, table string) {
	t.Helper()
	rows, err := database.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("inspect %s: %v", table, err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		if strings.HasSuffix(name, "_at_ms") && strings.ToUpper(dataType) != "INTEGER" {
			t.Errorf("%s.%s uses %s, want INTEGER", table, name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
}
