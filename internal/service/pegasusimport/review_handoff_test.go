package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type handoffMemory struct {
	err     error
	commits int
}

func (memory *handoffMemory) CommitReviewHandoff(
	_ context.Context, _ model.ReviewHandoffRequest, _ int64,
	_, _ string, _, _ *string, _ int,
) error {
	memory.commits++
	return memory.err
}

func handoffClock() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func newHandoffMemoryService(memory *handoffMemory) *ReviewHandoff {
	return NewReviewHandoff(memory, nil, handoffClock)
}

func TestReviewHandoffCallsCommit(t *testing.T) {
	t.Parallel()
	memory := &handoffMemory{}
	request := model.ReviewHandoffRequest{
		ItemID: "item", ImportID: "import", JobID: "job",
		LibraryJobID: "library-job", LibraryItemID: "library-item",
		ExecutionNo: 2, Attempt: 3, WorkerID: "worker",
	}
	if err := newHandoffMemoryService(memory).Complete(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if memory.commits != 1 {
		t.Fatalf("commits=%d", memory.commits)
	}
}

func TestReviewHandoffPreservesStorageCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("handoff failed")
	memory := &handoffMemory{err: cause}
	request := model.ReviewHandoffRequest{
		ItemID: "item", ImportID: "import", JobID: "job",
		LibraryJobID: "library-job", LibraryItemID: "library-item",
		ExecutionNo: 2, Attempt: 3, WorkerID: "worker",
	}
	if err := newHandoffMemoryService(memory).Complete(t.Context(), request); !errors.Is(err, cause) {
		t.Fatalf("lost cause=%v", err)
	}
}
