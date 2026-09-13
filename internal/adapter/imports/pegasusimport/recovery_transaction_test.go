package pegasusimport

import (
	"context"
	"errors"
	"testing"

	repository "retrom/internal/repo/pegasusimport"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"
)

type failedRecovery struct {
	application.RecoveryRepository
	cause error
}

func (failure failedRecovery) WithRecovery(ctx context.Context, work func(application.RecoveryScope) error) error {
	return failure.RecoveryRepository.WithRecovery(ctx, func(scope application.RecoveryScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return failure.cause
	})
}

func TestRecoveryRollsBackSeededReviewAndParentOnLateFailure(t *testing.T) {
	t.Parallel()
	service, _, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=5 WHERE id='work'`)
	before := readHandoffState(t, service)
	cause := errors.New("recovery commit rejected")
	storage := failedRecovery{RecoveryRepository: repository.NewRecovery(service.database), cause: cause}
	recovery := application.NewRecovery(storage, library.NewMetadataSeeder(nil, service.now), service.now)
	if err := recovery.Recover(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("late failure lost cause: %v", err)
	}
	if after := readHandoffState(t, service); after != before {
		t.Fatalf("partial review recovery: before=%#v after=%#v", before, after)
	}
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id='work'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("failed recovery closed job: %s", state)
	}
}

func TestRecoveryRequeuesAfterPreservingCreatedReviewExactlyOnce(t *testing.T) {
	t.Parallel()
	service, _, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=5 WHERE id='work'`)
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := readHandoffState(t, service)
	if before.State != "REVIEW_PENDING" || before.DraftVersion != 2 || before.Audits != 1 {
		t.Fatalf("lost review: %#v", before)
	}
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after := readHandoffState(t, service); after != before {
		t.Fatalf("duplicate recovery: %#v", after)
	}
}
