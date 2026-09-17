package metadatascrape

import (
	"context"
	"database/sql/driver"
	"errors"
	metadatascrapemodel "retrom/internal/model/metadatascrape"
	metadatascrapeservice "retrom/internal/service/metadatascrape"
	"retrom/internal/testkit/testsupport"
	"strings"
	"testing"
)

func TestMetadataRecoveredLeaseAndRetryEventRollbackTogether(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='old-worker',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=? WHERE id='job'`, now-100000, now+10000, now-1)
	cause := errors.New("retry event storage failed")
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
		if strings.Contains(query, "INSERT INTO job_events") && len(args) == 4 && args[0].Value == "RETRY_SCHEDULED" && args[3].Value == "job" {
			hits++
			return cause
		}
		return nil
	}})
	processor := recoveryProcess(func(context.Context, metadatascrapemodel.WorkerClaim, string) (int, string, error) {
		t.Fatal("failed recovery processed")
		return 0, "", nil
	})
	err := metadatascrapeservice.NewWorker(NewWorker(fault), processor, recoveryNow).Run(t.Context(), "run")
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("recovery error=%v hits=%d", err, hits)
	}
	var state, worker string
	var attempts int
	if err := database.QueryRowContext(t.Context(), `SELECT state,worker_id,attempt_count FROM jobs WHERE id='job'`).Scan(&state, &worker, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || worker != "old-worker" || attempts != 1 {
		t.Fatalf("partial recovery=%s/%s/%d", state, worker, attempts)
	}
}

func TestMetadataRecoveryScanPreservesStorageCause(t *testing.T) {
	database := recoveryDatabase(t)
	cause := errors.New("recovery scan failed")
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
		if strings.Contains(query, "SELECT r.id FROM metadata_scrape_runs") && len(args) == 4 && args[0].Value == recoveryTime.UnixMilli() {
			hits++
			return cause
		}
		return nil
	}})
	_, err := metadatascrapeservice.NewWorker(NewWorker(fault), nil, recoveryNow).Recover(t.Context())
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("scan error=%v hits=%d", err, hits)
	}
}
