package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
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
	*completionMemory
	links     *payloadLinksMemory
	committed bool
}

func (memory *completionPayloadMemory) WithCompletion(_ context.Context, run func(model.CompletionScope) error) error {
	if err := run(model.CompletionScope{Read: memory.completionMemory, Write: memory.completionMemory, Payload: payloadreleasemodel.ReleaseScope{Links: memory.links}}); err != nil {
		return err
	}
	memory.committed = true
	return nil
}

func TestCompletionRollsBackWhenPayloadLinksCannotBeRead(t *testing.T) {
	t.Parallel()
	cause := errors.New("payload links read unavailable")
	memory := &completionPayloadMemory{completionMemory: newCompletionMemory(), links: &payloadLinksMemory{cause: cause}}
	err := NewCompletion(memory, func() time.Time { return time.UnixMilli(2000) }).Finish(t.Context(), memory.before.Execution)
	if !errors.Is(err, cause) || memory.committed || memory.links.calls != 1 {
		t.Fatalf("payload error lost: %v committed=%t calls=%d", err, memory.committed, memory.links.calls)
	}
}
