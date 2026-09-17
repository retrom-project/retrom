//go:build integration

package metadatascrape_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
	initialpersistence "retrom/internal/repo/metadatascrape"
	metadatascrapeservice "retrom/internal/service/metadatascrape"
)

type failingInitialWriter struct {
	metadatascrapemodel.InitialWriter
}

func (writer failingInitialWriter) Advance(ctx context.Context, change metadatascrapemodel.InitialProgressChange) error {
	if err := writer.InitialWriter.Advance(ctx, change); err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func assertInitialProgressRollback(t *testing.T, database *sql.DB, runID, itemID string) {
	t.Helper()
	before := readInitialProgress(t, database, itemID)
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	scope := initialpersistence.BindInitialReview(transaction)
	scope.Write = failingInitialWriter{scope.Write}
	err = metadatascrapeservice.NewInitialReview(scope).Complete(t.Context(), runID, 100)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late initial progress failure: %v", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if after := readInitialProgress(t, database, itemID); after != before {
		t.Fatalf("partial progress committed: before=%+v after=%+v", before, after)
	}
}

type initialProgress struct {
	itemState, jobState string
	values              [5]int64
}

func readInitialProgress(t *testing.T, database *sql.DB, itemID string) initialProgress {
	t.Helper()
	var value initialProgress
	err := database.QueryRowContext(t.Context(), `SELECT i.state,j.state,i.version,j.version,j.running_item_count,
 j.failed_item_count,j.review_pending_item_count FROM import_items i JOIN import_jobs j ON j.id=i.import_job_id WHERE i.id=?`, itemID).
		Scan(&value.itemState, &value.jobState, &value.values[0], &value.values[1], &value.values[2], &value.values[3], &value.values[4])
	if err != nil {
		t.Fatal(err)
	}
	return value
}
