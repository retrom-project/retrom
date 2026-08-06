package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
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
	if err := json.Unmarshal(contents, &supported); err != nil || supported == nil || len(supported) != 0 {
		t.Fatalf("greenfield supported versions = %#v, error=%v", supported, err)
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
