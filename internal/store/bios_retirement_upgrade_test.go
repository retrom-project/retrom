package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestBIOSRetirementUpgradePreservesLaunchesAndIndexesPendingWork(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	old := openMigrationTestDatabase(t, path)
	sources, err := migrationSources()
	testassert.False(t, err != nil, err)
	for _, source := range sources[:12] {
		testassert.False(t, runMigration(t.Context(), old, source, time.Now) != nil, source.name)
	}
	seedCurrentRuntimeGraph(t, old)
	_, err = old.ExecContext(t.Context(), currentLaunchInsertSQL, "current-launch", "current-game-a", "target-a")
	testassert.False(t, err != nil, err)
	testassert.False(t, old.Close() != nil, "close old database")
	current, err := Open(t.Context(), path, time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { testassert.False(t, current.Close() != nil, "close database") })
	var due int64
	var released sql.NullInt64
	err = current.SQL.QueryRowContext(t.Context(), `SELECT due_at_ms,released_at_ms FROM launch_payload_retirements WHERE launch_session_id='current-launch'`).Scan(&due, &released)
	testassert.False(t, err != nil, err)
	testassert.True(t, due == 10 && !released.Valid, "upgrade lost pending bootstrap")
	_, err = current.SQL.ExecContext(t.Context(), `UPDATE launch_sessions SET state='ACTIVE',activated_at_ms=2,idle_expires_at_ms=15,updated_at_ms=2,version=version+1 WHERE id='current-launch'`)
	testassert.False(t, err != nil, err)
	err = current.SQL.QueryRowContext(t.Context(), `SELECT due_at_ms FROM launch_payload_retirements WHERE launch_session_id='current-launch'`).Scan(&due)
	testassert.False(t, err != nil, err)
	testassert.True(t, due == 15, "heartbeat did not move retirement deadline")
	for _, query := range []struct{ sql, index string }{
		{`SELECT launch_session_id FROM launch_payload_retirements WHERE released_at_ms IS NULL AND due_at_ms<=100 ORDER BY due_at_ms,launch_session_id LIMIT 200`, "launch_payload_retirement"},
		{`SELECT id,blob_id FROM bios_installations WHERE is_active=0 AND blob_id IS NOT NULL ORDER BY updated_at_ms,id LIMIT 1`, "bios_installations_retirement"},
		{`SELECT rowid FROM variant_files WHERE role='BIOS_BUNDLE' AND blob_id='old' LIMIT 200`, "variant_files_bios_blob"},
	} {
		assertRetirementIndex(t, current.SQL, query.sql, query.index)
	}
	testassert.False(t, current.IntegrityCheck(t.Context()) != nil, "upgrade integrity")
}

func assertRetirementIndex(t *testing.T, database *sql.DB, query, index string) {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query)
	testassert.False(t, err != nil, err)
	defer func() { testassert.False(t, rows.Close() != nil, "close plan") }()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		testassert.False(t, rows.Scan(&id, &parent, &unused, &detail) != nil, "read plan")
		plan.WriteString(detail)
	}
	testassert.False(t, rows.Err() != nil, "iterate plan")
	testassert.True(t, strings.Contains(plan.String(), index), plan.String())
}
