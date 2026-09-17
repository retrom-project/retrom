package serverimport_test

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/serverimport"

	importpersistence "retrom/internal/repo/serverimport"
	importservice "retrom/internal/service/serverimport"
)

type failingLeaseRepository struct{ model.LeaseRepository }

func (repository failingLeaseRepository) CommitWrite(ctx context.Context, work func(model.LeaseRecords) error) error {
	return repository.LeaseRepository.CommitWrite(ctx, func(records model.LeaseRecords) error {
		if err := work(records); err != nil {
			return err
		}
		return context.Canceled
	})
}

func TestLeaseClaimFailureRollsBackOwnerBudgetAndEvents(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	created, err := legacy.Create(t.Context(), model.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	service := importservice.NewLeases(failingLeaseRepository{importpersistence.NewLeases(database)}, legacy.NowForTest)
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

func TestProgressConflictRollsBackJobLeaseAndEvent(t *testing.T) {
	legacy, database, _ := archiveImportFixture(t)
	created, err := legacy.Create(t.Context(), model.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	unit, found, err := legacy.ClaimForTest(t.Context())
	if err != nil || !found {
		t.Fatalf("claim: %v %v", found, err)
	}
	repository := importpersistence.NewLeases(database)
	var before model.LeaseSnapshot
	if err := repository.CommitWrite(t.Context(), func(records model.LeaseRecords) error {
		var err error
		before, err = records.Current(t.Context(), unit.JobID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_imports SET version=version+1 WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	err = repository.CommitWrite(t.Context(), func(records model.LeaseRecords) error {
		return records.Touch(t.Context(), model.LeaseTouch{Before: before, Now: legacy.NowForTest().UnixMilli(), LeaseUntil: *before.LeaseUntil + 1000, Phase: "DISCOVERING", Event: []byte(`{"schemaVersion":1}`)})
	})
	if !errors.Is(err, model.ErrLeaseLost) {
		t.Fatalf("stale progress accepted: %v", err)
	}
	importVersion, jobVersion := workerVersions(t, database, unit)
	var lease, events int64
	err = database.QueryRowContext(t.Context(), `SELECT leased_until_ms,
(SELECT count(*) FROM job_events WHERE job_id=jobs.id AND event_type='PROGRESS') FROM jobs WHERE id=?`, unit.JobID).Scan(&lease, &events)
	if err != nil || importVersion != before.ImportVersion+1 || jobVersion != before.JobVersion || lease != *before.LeaseUntil || events != 0 {
		t.Fatalf("partial progress: import=%d job=%d lease=%d events=%d %v", importVersion, jobVersion, lease, events, err)
	}
}
