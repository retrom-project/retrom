package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	model "retrom/internal/model/pegasusimport"
)

type payloadLinksMemory struct {
	cause error
	calls int
}

func (memory *payloadLinksMemory) RetainedSources(context.Context, payloadreleasemodel.SourceBatch, string, int) ([]string, error) {
	memory.calls++
	return nil, memory.cause
}

func (*payloadLinksMemory) BoundSources(context.Context, string, payloadreleasemodel.Scope, int) ([]payloadreleasemodel.Scope, error) {
	return nil, nil
}

func emptyPayloadScope() payloadreleasemodel.ReleaseScope {
	return payloadreleasemodel.ReleaseScope{Links: &payloadLinksMemory{}}
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

func (memory *completionPayloadMemory) Payload() payloadreleasemodel.ReleaseScope {
	return payloadreleasemodel.ReleaseScope{Links: memory.links}
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
