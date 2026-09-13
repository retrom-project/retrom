package payloadrelease

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPayloadStartIsIdempotentAndCloseCancelsActiveWork(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	_, err := fixture.database.ExecContext(t.Context(), `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,
execution_no,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES('pending-mutation','GAME','schedule-game','GAME_CONTENT_REPLACE',
'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',1,'{}',0,'QUEUED',0,4,10,10,10)`)
	if err != nil {
		t.Fatal(err)
	}
	entered, force := make(chan struct{}), make(chan struct{})
	var enteredOnce, forceOnce sync.Once
	unblock := func() { forceOnce.Do(func() { close(force) }) }
	defer unblock()
	fixture.service.waitFor = func(ctx context.Context, _ time.Duration) error {
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-force:
			return context.Canceled
		}
	}
	fixture.service.Start()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("release worker did not reach actual mutation wait")
	}
	before := releaseJobAuthority(t, fixture.database, fixture.jobID)
	fixture.service.Start()
	if after := releaseJobAuthority(t, fixture.database, fixture.jobID); after != before {
		t.Errorf("second Start replaced its own active execution: before=%+v after=%+v", before, after)
	}
	closed := make(chan struct{})
	go func() { fixture.service.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Error("Close left active work running until its full execution timeout")
		unblock()
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("release worker did not stop after fixture unblocked")
		}
	}
}
