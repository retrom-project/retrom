//go:build integration

package metadatascrape_test

import (
	"database/sql"
	"testing"

	"retrom/internal/service/metadatascrape"
)

func assertForeignMetadataExecutionIsUntouched(t *testing.T, database *sql.DB, scraper *metadatascrape.Service, runID, jobID string) {
	t.Helper()
	now := mediaFixtureNow().UnixMilli()
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET state='RUNNING',worker_id='another-worker',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=? WHERE id=?`, now, now+3600000, now+60000, now, jobID); err != nil {
		t.Fatal(err)
	}
	if err := scraper.Run(t.Context(), runID); err != nil {
		t.Fatalf("foreign execution must be ignored: %v", err)
	}
	var state, worker string
	var events int
	if err := database.QueryRowContext(t.Context(), `SELECT state,worker_id,(SELECT count(*) FROM job_events WHERE job_id=jobs.id AND event_type<>'QUEUED') FROM jobs WHERE id=?`, jobID).Scan(&state, &worker, &events); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || worker != "another-worker" || events != 0 {
		t.Fatalf("foreign execution changed: state=%s worker=%s events=%d", state, worker, events)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE jobs SET state='QUEUED',worker_id=NULL,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL WHERE id=?`, jobID); err != nil {
		t.Fatal(err)
	}
}
