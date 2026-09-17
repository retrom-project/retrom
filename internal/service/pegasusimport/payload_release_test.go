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

func (memory *completionPayloadMemory) WithCompletion(_ context.Context, run func(model.CompletionRecords) error) error {
	if err := run(memory); err != nil {
		return err
	}
	memory.committed = true
	return nil
}

func (memory *completionPayloadMemory) Payload() payload.ReleaseScope {
	return payload.ReleaseScope{Links: memory.links}
}

func TestCompletionRollsBackWhenPayloadLinksCannotBeRead(t *testing.T) {
	t.Parallel()
	cause := errors.New("payload links read unavailable")
	completion, identity := completionFixture()
	memory := &completionPayloadMemory{completionFake: completion, links: &payloadLinksMemory{cause: cause}}
	err := NewCompletion(memory, func() time.Time { return time.UnixMilli(10) }).Finish(t.Context(), identity)
	if !errors.Is(err, cause) || memory.committed || memory.links.calls != 1 {
		t.Fatalf("payload error lost: %v committed=%t calls=%d", err, memory.committed, memory.links.calls)
	}
}
