package workflowfixture

import (
	"testing"
	"time"

	"retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
)

// DeleteFinishedProcess proves product consumers can run with no workflow records.
// Job execution history may remain; it has no product authority or artifact data.
func DeleteFinishedProcess(t *testing.T, database dbapi.DB, files *filestore.Store, itemID string) {
	t.Helper()
	var importID, uploadID string
	var now int64
	if err := dbapi.QueryRowContext(t.Context(), database, `
SELECT item.import_job_id,job.upload_session_id,item.updated_at_ms
 FROM import_items item JOIN import_jobs job ON job.id=item.import_job_id WHERE item.id=?`,
		itemID).Scan(&importID, &uploadID, &now); err != nil {
		t.Fatal(err)
	}
	collector, err := cleanupjobs.New(t.Context(), database, files, func() time.Time { return time.UnixMilli(now) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(collector.Close)
	for attempt := 0; attempt < 100; attempt++ {
		now += 1000
		if _, err := collector.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		var remaining int
		if err := dbapi.QueryRowContext(t.Context(), database,
			`SELECT (SELECT count(*) FROM import_items WHERE import_job_id=? AND payload_state<>'RELEASED')
 + (SELECT count(*) FROM import_jobs WHERE id=? AND payload_state<>'RELEASED')`,
			importID, importID).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining == 0 {
			break
		}
		if attempt == 99 {
			t.Fatal("completed import payload did not finish cleanup")
		}
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM import_item_duplicate_matches WHERE import_item_id IN (
SELECT id FROM import_items WHERE import_job_id=?)`, []any{importID}},
		{`DELETE FROM import_job_file_resolutions WHERE import_job_id=?
OR replacement_import_job_id=?`, []any{importID, importID}},
		{`DELETE FROM import_job_files WHERE import_job_id=?`, []any{importID}},
		{`DELETE FROM import_group_requests WHERE import_job_id=?`, []any{importID}},
		{`DELETE FROM import_items WHERE import_job_id=?`, []any{importID}},
		{`DELETE FROM import_jobs WHERE id=?`, []any{importID}},
		{`DELETE FROM upload_consumptions WHERE upload_session_id=?`, []any{uploadID}},
		{`DELETE FROM import_files WHERE upload_session_id=?`, []any{uploadID}},
		{`DELETE FROM upload_parts WHERE upload_file_id IN (
SELECT id FROM upload_files WHERE upload_session_id=?)`, []any{uploadID}},
		{`DELETE FROM upload_files WHERE upload_session_id=?`, []any{uploadID}},
		{`DELETE FROM upload_sessions WHERE id=?`, []any{uploadID}},
	} {
		if _, err := tx.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatalf("delete finished process: %s: %v", statement.query, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
