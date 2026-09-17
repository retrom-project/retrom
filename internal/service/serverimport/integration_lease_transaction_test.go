package serverimport_test

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/serverimport"

	importpersistence "retrom/internal/repo/serverimport"
	importservice "retrom/internal/service/serverimport"
)

func TestLeaseClaimFailureRollsBackOwnerBudgetAndEvents(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	created, err := legacy.Create(t.Context(), model.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	repo := importpersistence.NewLeases(database).WithPreCommitHook(func() error { return context.Canceled })
	service := importservice.NewLeases(repo, legacy.NowForTest)
	unit, found, err := service.Claim(t.Context())
	if !errors.Is(err, context.Canceled) || found || unit.Owner != "" {
		t.Fatalf("failed claim: %+v %v %v", unit, found, err)
	}
	var state string
	var version, attempt, leases, deadlines, events int64
	err = database.QueryRowContext(t.Context(), `SELECT state,version,attempt_count,worker_id IS NOT NULL,
execution_deadline_at_ms IS NOT NULL,(SELECT count(*) FROM job_events WHERE job_id=jobs.id AND event_type='STARTED')
FROM jobs WHERE id=?`, created.JobID).Scan(&state, &version, &attempt, &leases, &deadlines, &events)
	if err != nil || state != "QUEUED" || version != 1 || attempt != 0 || leases != 0 || deadlines != 0 || events != 0 {
		t.Fatalf("partial claim: %s/%d attempt=%d lease=%d deadline=%d events=%d %v", state, version, attempt, leases, deadlines, events, err)
	}
	current, err := legacy.Get(t.Context(), created.ID)
	if err != nil || current.State != "QUEUED" || current.Version != created.Version {
		t.Fatalf("partial import claim: %+v %v", current, err)
	}
}

func TestProgressCancelledImportRollsBackJobLeaseAndEvent(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	created, err := legacy.Create(t.Context(), model.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	unit, found, err := legacy.ClaimForTest(t.Context())
	if err != nil || !found {
		t.Fatalf("claim: %v %v", found, err)
	}

	if _, _, err := legacy.Cancel(t.Context(), created.ID, created.Version+1, "stop", controlActorID); err != nil {
		t.Fatal(err)
	}

	repository := importpersistence.NewLeases(database)
	err = repository.CommitTouch(t.Context(), model.TouchCommand{
		Unit: unit, Phase: "DISCOVERING",
		Event: []byte(`{"schemaVersion":1}`),
		Now:   legacy.NowForTest().UnixMilli(),
	})
	if !errors.Is(err, model.ErrWorkerCancelled) {
		t.Fatalf("cancelled progress accepted: %v", err)
	}
	var events int64
	err = database.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM job_events WHERE job_id=jobs.id AND event_type='PROGRESS') FROM jobs WHERE id=?`, unit.JobID).Scan(&events)
	if err != nil || events != 0 {
		t.Fatalf("partial progress: events=%d %v", events, err)
	}
}
