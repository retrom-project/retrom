package emulationstationimport

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestESExpiredReviewRecoveryRollsBackMetadataAndOwnership(t *testing.T) {
	for _, statement := range []string{
		"UPDATE review_drafts SET", "INSERT INTO review_events(",
		"UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING'",
		"UPDATE jobs SET state=", "INSERT INTO job_events", "callback", "affected rows", "zero rows",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			started, unit := startLifecycleImport(t, fixture, "", "nes")
			item, imported := reserveExecutionReview(t, fixture, unit)
			requestExecutionCancellation(t, fixture, started.ID)
			*fixture.now = fixture.now.Add(time.Minute)
			before := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID)
			var hits atomic.Int64
			hook := executionReviewFaultHook(statement, item.ID, unit.JobID, imported.Items[0].ItemID, &hits)
			faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, recoveryReviewHooks(statement, item.ID, hook, &hits))
			var repository application.RecoveryRepository = persistence.NewRecovery(faultDB)
			if statement == "callback" {
				repository = recoveryReviewCallback{repository}
			}
			err := application.NewRecovery(repository, fixture.service.now).Recover(fixture.context)
			want := errExecutionReviewFault
			if statement == "zero rows" {
				want = nil
			}
			if !errors.Is(err, want) {
				t.Fatalf("recovery cause=%v", err)
			}
			if statement != "callback" && hits.Load() != 1 {
				t.Fatalf("fault hits=%d", hits.Load())
			}
			if after := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID); after != before {
				t.Fatalf("partial recovery before=%s after=%s", before, after)
			}
		})
	}
}

type recoveryReviewCallback struct{ application.RecoveryRepository }

func (repository recoveryReviewCallback) WithRecovery(ctx context.Context, run func(application.RecoveryScope) error) error {
	return repository.RecoveryRepository.WithRecovery(ctx, func(scope application.RecoveryScope) error {
		if err := run(scope); err != nil {
			return err
		}
		return errExecutionReviewFault
	})
}
