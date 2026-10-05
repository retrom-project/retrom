package libraryimport

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestBulkHeartbeatRetriesWithoutDoubleCountingProgress(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	claimRetryBulk(t, database)
	var conflicted atomic.Bool
	faultDatabase := testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{
		BeforeExec: func(ctx context.Context, query string, _ []driver.NamedValue) error {
			if !strings.Contains(query, "UPDATE jobs SET heartbeat_at_ms=") ||
				!conflicted.CompareAndSwap(false, true) {
				return nil
			}
			// A competing writer commits after the bulk cursor update but before
			// its heartbeat. Both writes must roll back and retry together.
			_, err := database.ExecContext(ctx,
				"UPDATE jobs SET updated_at_ms=updated_at_ms WHERE id='bulk-job'")
			return err
		},
	})
	attempts := 0
	if err := NewReviewBulk(faultDatabase).WithStep(t.Context(), func(step libraryservice.ReviewBulkStep) error {
		attempts++
		item, found, err := step.Worker.Next(t.Context(), "bulk", "worker")
		if err != nil {
			return err
		}
		if !found || item.ID != "item" {
			return fmt.Errorf("retry lost pending item: %#v found=%v", item, found)
		}
		return step.Worker.Skip(t.Context(), "bulk", "bulk-job", "worker", item.ID, "NOT_READY", 12)
	}); err != nil {
		t.Fatal(err)
	}
	var scanned, skipped, version, heartbeat int64
	if err := dbapi.QueryRowContext(t.Context(), database, `
SELECT bulk.scanned_count,bulk.skipped_not_ready_count,job.version,job.heartbeat_at_ms
FROM review_bulk_approvals bulk JOIN jobs job ON job.id=bulk.job_id WHERE bulk.id='bulk'`).Scan(
		&scanned, &skipped, &version, &heartbeat); err != nil {
		t.Fatal(err)
	}
	if !conflicted.Load() || attempts != 2 || scanned != 1 || skipped != 1 || version != 3 || heartbeat != 12 {
		t.Fatalf("conflicted=%v attempts=%d scanned=%d skipped=%d version=%d heartbeat=%d",
			conflicted.Load(), attempts, scanned, skipped, version, heartbeat)
	}
}

func claimRetryBulk(t *testing.T, database dbapi.DB) {
	t.Helper()
	if err := NewReviewBulk(database).WithStep(t.Context(), func(step libraryservice.ReviewBulkStep) error {
		if _, err := step.Writes.CreateGlobal(t.Context(), "bulk", "bulk-job", "actor", 10); err != nil {
			return err
		}
		_, _, err := step.Worker.Claim(t.Context(), "bulk", "worker", 11)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
