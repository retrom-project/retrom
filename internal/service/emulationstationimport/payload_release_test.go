package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"

	payload "retrom/internal/model/payloadrelease"
)

type payloadLinksMemory struct {
	cause error
	calls int
}

func (memory *payloadLinksMemory) RetainedSources(context.Context, payload.SourceBatch, string, int) ([]string, error) {
	memory.calls++
	return nil, memory.cause
}

func (*payloadLinksMemory) BoundSources(context.Context, string, payload.Scope, int) ([]payload.Scope, error) {
	return nil, nil
}

func emptyPayloadScope() payload.ReleaseScope {
	return payload.ReleaseScope{Links: &payloadLinksMemory{}}
}

type completionPayloadMemory struct {
	*completionMemory
	links     *payloadLinksMemory
	committed bool
}

func (memory *completionPayloadMemory) CommitCompletion(_ context.Context, unit model.Execution, nowMS int64) error {
	if memory.readErr != nil {
		return memory.readErr
	}
	change, err := model.PlanCompletion(memory.before, memory.counts, nowMS)
	if err != nil {
		return err
	}
	if memory.links.cause != nil {
		return memory.links.cause
	}
	memory.change = change
	memory.committed = true
	return nil
}

func TestCompletionRollsBackWhenPayloadLinksCannotBeRead(t *testing.T) {
	t.Parallel()
	cause := errors.New("payload links read unavailable")
	memory := &completionPayloadMemory{completionMemory: newCompletionMemory(), links: &payloadLinksMemory{cause: cause}}
	err := NewCompletion(memory, func() time.Time { return time.UnixMilli(2000) }).Finish(t.Context(), memory.before.Execution)
	if !errors.Is(err, cause) || memory.committed || memory.links.calls != 0 {
		t.Fatalf("payload error lost: %v committed=%t calls=%d", err, memory.committed, memory.links.calls)
	}
}
