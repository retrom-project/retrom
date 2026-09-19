package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"

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
	*completionFake
	links     *payloadLinksMemory
	committed bool
}

func (memory *completionPayloadMemory) CommitCompletion(ctx context.Context, identity model.ExecutionIdentity, nowMS int64) error {
	if err := model.ValidateExecution(memory.before, identity, nowMS); err != nil {
		return err
	}
	if memory.before.Kind != "SERVER_PEGASUS_IMPORT" || memory.before.JobState != "RUNNING" {
		return model.ErrVersionConflict
	}
	if memory.counts.Unfinished != 0 {
		return model.ErrVersionConflict
	}
	if memory.links.cause != nil {
		return memory.links.cause
	}
	memory.committed = true
	return nil
}

func TestCompletionRollsBackWhenPayloadLinksCannotBeRead(t *testing.T) {
	t.Parallel()
	cause := errors.New("payload links read unavailable")
	completion, identity := completionFixture()
	memory := &completionPayloadMemory{completionFake: completion, links: &payloadLinksMemory{cause: cause}}
	err := NewCompletion(memory, func() time.Time { return time.UnixMilli(10) }).Finish(t.Context(), identity)
	if !errors.Is(err, cause) || memory.committed || memory.links.calls != 0 {
		t.Fatalf("payload error lost: %v committed=%t calls=%d", err, memory.committed, memory.links.calls)
	}
}
