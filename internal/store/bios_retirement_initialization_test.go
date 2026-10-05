package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"

	"retrom/internal/testassert"
)

func TestFreshBIOSRetirementTracksLaunchesAndIndexesPendingWork(t *testing.T) {
	t.Parallel()
	current, err := Open(t.Context(), testpostgres.DSN(t), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { testassert.False(t, current.Close() != nil, "close fresh database") })
	seedCurrentRuntimeGraph(t, current.SQL)
	tx := lifecycleTransaction(t, current.SQL)
	_, err = sessionstore.CreateLaunch(t.Context(), tx, currentLaunchInsertSQL, "current-launch",
		"current-game-a", "target-a")
	testassert.False(t, err != nil, err)
	testassert.False(t, tx.Commit() != nil, "commit launch")
	var due int64
	var released sql.NullInt64
	err = dbapi.QueryRowContext(t.Context(), current.SQL, `SELECT due_at_ms,released_at_ms FROM launch_payload_retirements WHERE launch_session_id='current-launch'`).Scan(&due, &released)
	testassert.False(t, err != nil, err)
	testassert.True(t, due == 10 && !released.Valid, "creation lost pending bootstrap")
	_, err = sessionstore.ChangeLaunch(t.Context(), current.SQL, recordstore.Update{
		Set: `
state='ACTIVE',activated_at_ms=2,updated_at_ms=2,version=version+1
`,
		Scope: recordstore.Scope{
			Where: `id='current-launch'`,
		},
	})
	testassert.False(t, err != nil, err)
	err = dbapi.QueryRowContext(t.Context(), current.SQL, `SELECT due_at_ms FROM launch_payload_retirements WHERE launch_session_id='current-launch'`).Scan(&due)
	testassert.False(t, err != nil, err)
	testassert.True(t, due == 20, "active launch did not use hard retirement deadline")
	for _, query := range []struct{ sql, index string }{
		{`SELECT launch_session_id FROM launch_payload_retirements WHERE released_at_ms IS NULL AND due_at_ms<=100
ORDER BY due_at_ms,launch_session_id LIMIT 200`, "launch_payload_retirement"},
		{`SELECT id,file_record FROM bios_installations WHERE is_active=0 AND file_record IS NOT NULL ORDER BY
updated_at_ms,id LIMIT 1`, "bios_installations_retirement"},
		{`SELECT game_variant_id FROM variant_files WHERE role='BIOS_BUNDLE' AND file_record='old' LIMIT 200`, "variant_files_bios_blob"},
	} {
		assertRetirementIndex(t, current.SQL, query.sql, query.index)
	}
	testassert.False(t, current.IntegrityCheck(t.Context()) != nil, "fresh integrity")
}

func assertRetirementIndex(t *testing.T, database dbapi.DB, query, index string) {
	t.Helper()
	tx, err := database.BeginTx(t.Context(), &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	if _, err := tx.ExecContext(t.Context(), "SET LOCAL enable_seqscan=off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(t.Context(), "EXPLAIN "+query)
	testassert.False(t, err != nil, err)
	defer func() { testassert.False(t, rows.Close() != nil, "close plan") }()
	var plan strings.Builder
	for rows.Next() {
		var detail string
		testassert.False(t, rows.Scan(&detail) != nil, "read plan")
		plan.WriteString(detail)
	}
	testassert.False(t, rows.Err() != nil, "iterate plan")
	testassert.True(t, strings.Contains(plan.String(), index), plan.String())
}
