package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type settlementMemory struct {
	before  model.ExecutionSnapshot
	change  model.WorkerSettlementChange
	failure error
	writes  int
}

func (m *settlementMemory) CurrentSettlement(_ context.Context, _ string) (model.ExecutionSnapshot, error) {
	return m.before, m.failure
}

func (m *settlementMemory) CommitSettlementReviewBatch(_ context.Context, _ model.ExecutionIdentity, _ int64, _ int) (model.SettlementReviewBatchResult, error) {
	if m.failure != nil {
		return model.SettlementReviewBatchResult{}, m.failure
	}
	return model.SettlementReviewBatchResult{Before: m.before, More: false}, nil
}

func (m *settlementMemory) CommitSettlement(_ context.Context, change model.WorkerSettlementChange) error {
	if m.failure != nil {
		return m.failure
	}
	m.writes++
	m.change = change
	return nil
}

func TestWorkerSettlementRejectsReplacedOwnerAndFailedStorage(t *testing.T) {
	t.Parallel()
	fixture, id := completionFixture()
	m := &settlementMemory{before: fixture.before}
	service := NewWorkerSettlement(m, nil, func() time.Time { return time.UnixMilli(10) })
	id.WorkerID = "replacement"
	if err := service.Fail(
		t.Context(),
		id,
		model.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
	); !errors.Is(
		err,
		model.ErrVersionConflict,
	) || m.writes != 0 {
		t.Fatalf("old owner failed=%v writes=%d", err, m.writes)
	}
	cause := errors.New("read failure")
	m.failure = cause
	if closed, err := service.Cancelled(t.Context(), id); closed || !errors.Is(err, cause) {
		t.Fatalf("cause=%v closed=%v", err, closed)
	}
}

func TestWorkerSettlementOnlyCancelsRequestedExecution(t *testing.T) {
	t.Parallel()
	fixture, id := completionFixture()
	m := &settlementMemory{before: fixture.before}
	service := NewWorkerSettlement(m, nil, func() time.Time { return time.UnixMilli(10) })
	if closed, err := service.Cancelled(t.Context(), id); closed || err != nil || m.writes != 0 {
		t.Fatalf("noncancel=%v %v", closed, err)
	}
	m.before.JobState = "CANCEL_REQUESTED"
	m.before.ImportState = "CANCEL_REQUESTED"
	if closed, err := service.Cancelled(
		t.Context(),
		id,
	); !closed || err != nil || m.change.State != "CANCELLED" || m.writes != 1 {
		t.Fatalf("cancel=%v %v %#v", closed, err, m.change)
	}
}
