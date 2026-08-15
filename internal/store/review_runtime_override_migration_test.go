package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
)

func TestReviewRuntimeOverrideMigrationUpgradesVersion32(t *testing.T) {
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
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 32)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, func() time.Time { return time.UnixMilli(2_000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := upgraded.SQL.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 35 {
		t.Fatalf("schema version = %d, error=%v", version, err)
	}
	for _, trigger := range []string{
		"review_preview_sessions_validate_insert",
		"review_runtime_screenshots_validate_insert",
		"review_runtime_screenshots_validate_update",
	} {
		var source string
		if err := upgraded.SQL.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, trigger).Scan(&source); err != nil {
			t.Fatalf("trigger %s: %v", trigger, err)
		}
		if strings.Contains(source, "status='READY'") || strings.Contains(source, "selected_validation_id=preview.validation_id") {
			t.Fatalf("trigger %s retained READY-only screenshot gate: %s", trigger, source)
		}
		if !strings.Contains(source, "prepublish_generation=4") {
			t.Fatalf("trigger %s lost current validation generation gate: %s", trigger, source)
		}
		if !strings.Contains(source, "ORDER BY candidate.created_at_ms DESC") {
			t.Fatalf("trigger %s does not bind the preview to the latest validation: %s", trigger, source)
		}
	}
}
