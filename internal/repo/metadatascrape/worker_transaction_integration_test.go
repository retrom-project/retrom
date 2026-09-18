//go:build integration

package metadatascrape_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
	workerpersistence "retrom/internal/repo/metadatascrape"
)

func assertMetadataClaimAndCompletionRollback(t *testing.T, database *sql.DB, runID, jobID, itemID string) {
	t.Helper()
	before := readInitialProgress(t, database, itemID)

	repository := workerpersistence.NewWorker(database)
	snapshot, readErr := repository.Run(t.Context(), runID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	now := mediaFixtureNow().UnixMilli()
	claimResult, claimErr := repository.CommitClaim(t.Context(), metadatascrapemodel.WorkerClaimCommand{
		Run:      snapshot,
		WorkerID: "transaction-test",
		Now:      now,
	})
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if !claimResult.Claimed {
		t.Fatal("metadata execution not claimed")
	}

	workerpersistence.WithWorkerPreCommitHook(repository, func() error {
		return context.Canceled
	})
	_, err := repository.CommitSettle(t.Context(), metadatascrapemodel.WorkerSettleCommand{
		Claim: claimResult.Claim,
		Now:   now,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late metadata completion failure: %v", err)
	}
	if after := readInitialProgress(t, database, itemID); after != before {
		t.Fatalf("partial initial review: before=%+v after=%+v", before, after)
	}
	var state, runState string
	var started, completed int
	if err := database.QueryRowContext(t.Context(), `SELECT j.state,r.state,
(SELECT count(*) FROM job_events WHERE job_id=j.id AND event_type='STARTED'),
(SELECT count(*) FROM job_events WHERE job_id=j.id AND event_type IN ('SUCCEEDED','FAILED'))
FROM jobs j JOIN metadata_scrape_runs r ON r.job_id=j.id WHERE j.id=?`, jobID).Scan(&state, &runState, &started, &completed); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || runState != "RUNNING" || started != 1 || completed != 0 {
		t.Fatalf("partial execution: state=%s run=%s started=%d completed=%d", state, runState, started, completed)
	}
}
