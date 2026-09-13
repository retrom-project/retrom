package serverimport_test

import (
	"context"
	"errors"
	"testing"

	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

type failingOutcomeRepository struct {
	importservice.OutcomeRepository
}

func (repository failingOutcomeRepository) WithWrite(ctx context.Context, work func(importservice.OutcomeScope) error) error {
	return repository.OutcomeRepository.WithWrite(ctx, func(scope importservice.OutcomeScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return context.Canceled
	})
}

func TestOutcomeLateFailureRollsBackItemRetryAndFailure(t *testing.T) {
	for _, action := range []string{"item", "retry", "failure"} {
		t.Run(action, func(t *testing.T) {
			legacy, database, unit, candidate := discoveryWriteFixture(t)
			service := importservice.NewOutcomes(failingOutcomeRepository{importpersistence.NewOutcomes(database)}, legacy.NowForTest)
			beforeImport, beforeJob := workerVersions(t, database, unit)
			var err error
			switch action {
			case "item":
				err = service.CompleteItem(t.Context(), unit, candidate.Item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
			case "retry":
				_, err = service.Fail(t.Context(), unit, "INTERNAL_ERROR")
			case "failure":
				_, err = service.Fail(t.Context(), unit, "SERVER_IMPORT_ROOT_CHANGED")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("late %s failure: %v", action, err)
			}
			afterImport, afterJob := workerVersions(t, database, unit)
			if beforeImport != afterImport || beforeJob != afterJob {
				t.Fatalf("partial %s versions: %d/%d %d/%d", action, beforeImport, afterImport, beforeJob, afterJob)
			}
			var itemState, jobState, importState string
			var events int64
			err = database.QueryRowContext(t.Context(), `SELECT item.state,job.state,import.state,
 (SELECT count(*) FROM job_events WHERE job_id=job.id AND event_type<>'QUEUED' AND event_type<>'STARTED')
 FROM server_bios_import_items item JOIN server_imports import ON import.id=item.server_import_id
 JOIN jobs job ON job.id=import.job_id WHERE import.id=?`, unit.ImportID).Scan(&itemState, &jobState, &importState, &events)
			if err != nil || itemState != "PENDING" || jobState != "RUNNING" || importState != "RUNNING" || events != 0 {
				t.Fatalf("partial %s outcome: %s/%s/%s events=%d %v", action, itemState, jobState, importState, events, err)
			}
		})
	}
}

func TestTerminalOutcomeLateFailurePreservesCurrentExecution(t *testing.T) {
	for _, action := range []string{"cancel", "finish"} {
		t.Run(action, func(t *testing.T) {
			legacy, database, unit, candidate := discoveryWriteFixture(t)
			prepareTerminalOutcome(t, legacy, unit, candidate, action)
			beforeImport, beforeJob := workerVersions(t, database, unit)
			before, err := legacy.Get(t.Context(), unit.ImportID)
			if err != nil {
				t.Fatal(err)
			}
			service := importservice.NewOutcomes(failingOutcomeRepository{importpersistence.NewOutcomes(database)}, legacy.NowForTest)
			if action == "cancel" {
				err = service.Cancel(t.Context(), unit)
			} else {
				err = service.Finish(t.Context(), unit)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("late terminal failure: %v", err)
			}
			afterImport, afterJob := workerVersions(t, database, unit)
			after, err := legacy.Get(t.Context(), unit.ImportID)
			if err != nil || beforeImport != afterImport || beforeJob != afterJob || before.State != after.State || before.Counts != after.Counts {
				t.Fatalf("partial terminal outcome: before=%+v after=%+v %v", before, after, err)
			}
		})
	}
}

func prepareTerminalOutcome(t *testing.T, service *Service, unit work, candidate *evaluatedCandidate, action string) {
	t.Helper()
	if action == "cancel" {
		current, err := service.Get(t.Context(), unit.ImportID)
		if err != nil {
			t.Fatal(err)
		}
		if _, pending, err := service.Cancel(t.Context(), unit.ImportID, current.Version, "stop", controlActorID); err != nil || !pending {
			t.Fatalf("request cancellation: %v %v", pending, err)
		}
		return
	}
	if err := service.PersistCandidatesForTest(t.Context(), unit, map[string][]*evaluatedCandidate{candidate.Item.RequirementID: {candidate}}, walkCounts{}); err != nil {
		t.Fatal(err)
	}
	service.CompleteItemForTest(t.Context(), unit, candidate.Item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
}
