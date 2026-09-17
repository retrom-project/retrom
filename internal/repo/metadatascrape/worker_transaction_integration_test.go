//go:build integration

package metadatascrape_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
	workerpersistence "retrom/internal/repo/metadatascrape"
	metadatascrapeservice "retrom/internal/service/metadatascrape"
)

func assertMetadataClaimAndCompletionRollback(t *testing.T, database *sql.DB, runID, jobID, itemID string) {
	t.Helper()
	before := readInitialProgress(t, database, itemID)
	claim := metadatascrapemodel.WorkerClaim{RunID: runID, JobID: jobID, WorkerID: "transaction-test", ExecutionNo: 1, Now: mediaFixtureNow().UnixMilli()}
	claim.Deadline = claim.Now + 3600000
	repository := workerpersistence.NewWorker(database)
	snapshot, readErr := repository.Run(t.Context(), runID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	claim.Version = snapshot.Version
	claim.AttemptCount = snapshot.AttemptCount
	err := repository.CommitWrite(t.Context(), func(scope metadatascrapemodel.WorkerScope) error {
		claimed, err := scope.Leases.Claim(t.Context(), claim)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatal("metadata execution not claimed")
		}
		foreign := claim
		foreign.WorkerID = "foreign"
		status, err := scope.Leases.Status(t.Context(), foreign, claim.Now)
		if err != nil {
			return err
		}
		if status.State != "" {
			t.Fatal("foreign worker acquired status")
		}
		refreshed, err := scope.Leases.Refresh(t.Context(), claim, claim.Now+60000)
		if err != nil {
			return err
		}
		if refreshed {
			t.Fatal("expired lease renewed")
		}
		if err := metadatascrapeservice.NewInitialReview(scope.Initial).Complete(t.Context(), runID, claim.Now); err != nil {
			return err
		}
		if err := scope.Write.Finish(t.Context(), metadatascrapemodel.WorkerOutcome{Claim: claim, State: "SUCCEEDED", RunState: "COMPLETED", Now: claim.Now}); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late metadata completion failure: %v", err)
	}
	if after := readInitialProgress(t, database, itemID); after != before {
		t.Fatalf("partial initial review: before=%+v after=%+v", before, after)
	}
	var state, runState string
	var events int
	if err := database.QueryRowContext(t.Context(), `SELECT j.state,r.state,(SELECT count(*) FROM job_events WHERE job_id=j.id AND event_type<>'QUEUED') FROM jobs j JOIN metadata_scrape_runs r ON r.job_id=j.id WHERE j.id=?`, jobID).Scan(&state, &runState, &events); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || runState != "RUNNING" || events != 0 {
		t.Fatalf("partial execution: state=%s run=%s events=%d", state, runState, events)
	}
}
