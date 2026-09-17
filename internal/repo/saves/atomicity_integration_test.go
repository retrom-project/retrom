//go:build integration

package saves

import (
	"context"
	"errors"
	"testing"
	"time"

	saveservice "retrom/internal/service/saves"
)

func TestCheckpointLateFailureRollsBackAllRecords(t *testing.T) {
	for _, updating := range []bool{false, true} {
		fixture := newGameSaveFixture(t)
		launch := fixture.createLaunch(t)
		if updating {
			syncGameData(t, fixture, launch, "original")
		}
		before := savePersistenceEvidence(t, fixture)
		failure := errors.New("idempotency storage failed")
		repository := lateSaveFailure{Repository: New(fixture.database.SQL), failure: failure}
		service := saveservice.New(repository, fixture.blobs, func() time.Time { return *fixture.now })
		_, _, err := service.CreateManual(fixture.ctx, launch.LaunchID, launch.Capability, "failed-write",
			manualRequest(t, "replacement", []byte("replacement"), screenshotPNG(t)))
		if !errors.Is(err, failure) {
			t.Fatalf("late failure was lost: %v", err)
		}
		after := savePersistenceEvidence(t, fixture)
		if after != before {
			t.Fatalf("updating=%v partial commit: before=%+v after=%+v", updating, before, after)
		}
	}
}

func savePersistenceEvidence(t *testing.T, fixture *saveFixture) [7]int64 {
	t.Helper()
	var evidence [7]int64
	err := fixture.database.SQL.QueryRowContext(fixture.ctx, `SELECT
 (SELECT count(*) FROM save_states), (SELECT count(*) FROM blobs),
 (SELECT count(*) FROM idempotency_records),
 (SELECT COALESCE(sum(data_version),0) FROM game_save_versions),
 (SELECT COALESCE(sum(expected_data_version),0) FROM launch_game_save_bindings),
 (SELECT COALESCE(sum(version),0) FROM save_states),
 (SELECT COALESCE(sum(payload_size_bytes),0) FROM save_states)`).
		Scan(&evidence[0], &evidence[1], &evidence[2], &evidence[3], &evidence[4], &evidence[5], &evidence[6])
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

type lateSaveFailure struct {
	saveservice.Repository
	failure error
}

func (repository lateSaveFailure) WithWrite(ctx context.Context, work func(saveservice.WriteScope) error) error {
	return repository.Repository.CommitWrite(ctx, func(scope saveservice.WriteScope) error {
		scope.Idempotency = lateSaveReplay{IdempotencyRecords: scope.Idempotency, failure: repository.failure}
		return work(scope)
	})
}

type lateSaveReplay struct {
	saveservice.IdempotencyRecords
	failure error
}

func (records lateSaveReplay) Remember(ctx context.Context, replay saveservice.ReplayWrite) error {
	if err := records.IdempotencyRecords.Remember(ctx, replay); err != nil {
		return err
	}
	return records.failure
}
