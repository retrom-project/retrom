package maintenance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func TestRestorePagesReviewsWithoutLosingTheLastBatch(t *testing.T) {
	t.Parallel()
	db := restoreReviewFixture(t, "PEGASUS")
	for number := 1; number < 205; number++ {
		addRestoredPegasusReview(t, db, number)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_imports SET game_count=205,processable_item_count=205
WHERE id='import'`); err != nil {
		t.Fatal(err)
	}
	var pages atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(query, "SELECT 'PEGASUS',source.id") && strings.Contains(query, "ORDER BY source.id LIMIT ?") {
				if len(args) != 2 || args[1].Value != int64(100) {
					return fmt.Errorf("restore exceeded bounded review page: %v", args)
				}
				pages.Add(1)
			}
			return nil
		},
	})
	if err := runReviewRestoreTransaction(t.Context(), faultDB); err != nil {
		t.Fatal(err)
	}
	var pending, failed, drafts, progress, games int
	err := db.QueryRowContext(t.Context(), `SELECT review_pending_item_count,failed_item_count,
(SELECT count(*) FROM review_events),(SELECT count(*) FROM job_events WHERE event_type='PROGRESS'),
(SELECT count(*) FROM games) FROM pegasus_imports WHERE id='import'`).Scan(
		&pending, &failed, &drafts, &progress, &games)
	if err != nil || pending != 205 || failed != 0 || drafts != 205 || progress != 205 || games != 0 || pages.Load() != 4 {
		t.Fatalf("restore batches: pending=%d failed=%d drafts=%d progress=%d games=%d reads=%d err=%v",
			pending, failed, drafts, progress, games, pages.Load(), err)
	}
}

func addRestoredPegasusReview(t *testing.T, db *sql.DB, number int) {
	t.Helper()
	id := fmt.Sprintf("copy-%04d", number)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,
expires_at_ms,created_at_ms,updated_at_ms)
SELECT ?,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms
FROM upload_sessions WHERE id='handoff-upload'`, []any{id + "-upload"}},
		{
			`INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
SELECT ?,?,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,
target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
review_pending_item_count,created_at_ms,updated_at_ms FROM import_jobs WHERE id='handoff-job'`,
			[]any{id + "-job", id + "-upload"},
		},
		{`INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,created_at_ms,updated_at_ms)
SELECT ?,?,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms
FROM import_items WHERE id='handoff-item'`, []any{id + "-review", id + "-job"}},
		{`INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,metadata_json,created_at_ms,updated_at_ms)
SELECT ?,?,target_platform_instance_id,metadata_json,created_at_ms,updated_at_ms
FROM review_drafts WHERE id='handoff-draft'`, []any{id + "-draft", id + "-review"}},
		{`INSERT INTO pegasus_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,
library_import_job_id,library_import_item_id,created_at_ms,updated_at_ms)
SELECT ?,import_id,metadata_relative_path,?,?,title,discovery_state,execution_state,metadata_json,
source_manifest_json,source_manifest_digest,?,?,created_at_ms,updated_at_ms
FROM pegasus_import_items WHERE id='item'`, []any{
			id + "-source", number, fmt.Sprintf("%064x", number),
			id + "-job", id + "-review",
		}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}
