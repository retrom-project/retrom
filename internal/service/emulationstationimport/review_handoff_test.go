package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type handoffMemory struct {
	before  model.LeaseSnapshot
	review  model.ExecutionReview
	err     error
	commits int
}

func (memory *handoffMemory) CommitReviewHandoff(
	_ context.Context, request model.ReviewHandoffRequest, nowMS int64,
	auditID, actorKind string, actorUserID, actorLabel *string,
) error {
	memory.commits++
	return memory.err
}

func newHandoffMemory() *handoffMemory {
	return &handoffMemory{
		before: newItemWorkMemory().before.Execution,
		review: model.ExecutionReview{ItemID: "source", State: "VALIDATING", Version: 2, LibraryJobID: "ordinary-job", LibraryItemID: "ordinary-item", ReservedJobID: "ordinary-job", ReservedItemID: "ordinary-item", MetadataJSON: `{"title":"Frozen title","releaseYear":2027}`, WarningsJSON: `[]`},
	}
}

func handoffMemoryService(memory *handoffMemory) *ReviewHandoff {
	now := func() time.Time { return time.UnixMilli(2000) }
	return NewReviewHandoff(memory, nil, now)
}

func handoffRequest(memory *handoffMemory) model.ReviewHandoffRequest {
	return model.ReviewHandoffRequest{
		Execution:     memory.before.Execution,
		ItemID:        memory.review.ItemID,
		LibraryJobID:  memory.review.ReservedJobID,
		LibraryItemID: memory.review.ReservedItemID,
	}
}

func TestReviewHandoffCallsCommit(t *testing.T) {
	memory := newHandoffMemory()
	if err := handoffMemoryService(memory).Complete(t.Context(), handoffRequest(memory)); err != nil {
		t.Fatal(err)
	}
	if memory.commits != 1 {
		t.Fatalf("commits=%d", memory.commits)
	}
}

func TestReviewHandoffPreservesStorageCause(t *testing.T) {
	cause := errors.New("handoff failed")
	memory := newHandoffMemory()
	memory.err = cause
	err := handoffMemoryService(memory).Complete(t.Context(), handoffRequest(memory))
	if !errors.Is(err, cause) {
		t.Fatalf("lost cause=%v", err)
	}
}
