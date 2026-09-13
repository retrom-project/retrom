package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type settlementMemory struct {
	before  ExecutionSnapshot
	change  WorkerSettlementChange
	failure error
	writes  int
}

func (m *settlementMemory) WithSettlement(_ context.Context, work func(WorkerSettlementScope) error) error {
	return work(WorkerSettlementScope{Payload: emptyPayloadScope(), Read: m, Write: m})
}

func (m *settlementMemory) Current(context.Context, string) (ExecutionSnapshot, error) {
	return m.before, m.failure
}

func (m *settlementMemory) Reviews(context.Context, string, int) ([]ReviewHandoffSnapshot, error) {
	return nil, m.failure
}

func (m *settlementMemory) CompleteReview(context.Context, RecoveryReviewChange) error {
	m.writes++
	return m.failure
}

func (m *settlementMemory) Close(_ context.Context, change WorkerSettlementChange) error {
	m.writes++
	m.change = change
	return m.failure
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
		ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
	); !errors.Is(
		err,
		ErrVersionConflict,
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
