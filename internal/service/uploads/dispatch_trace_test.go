package uploads

import (
	"context"
	"testing"
	"time"

	"retrom/internal/telemetry"
)

func TestFinalizationDoesNotAddTimingToHTTPRequest(t *testing.T) {
	ctx, request := telemetry.StartTrace(t.Context(), "http-request", "POST upload complete")
	repository := &timedWorkerRepository{traces: make(chan string, 2)}
	service := New(repository, nil, "", time.Now)
	t.Cleanup(service.Close)
	for range 2 {
		if !service.Resume(ctx, "job") {
			t.Fatal("finalization was not dispatched")
		}
		service.Wait()
	}
	first, second := <-repository.traces, <-repository.traces
	if first == "" || first == "http-request" || first == second {
		t.Fatalf("background traces were not isolated: %q, %q", first, second)
	}
	if elapsed := request.Duration(telemetry.WriterPoolWait); elapsed != 0 {
		t.Fatalf("background work polluted HTTP timing by %v", elapsed)
	}
}

type timedWorkerRepository struct {
	Repository
	traces chan string
}

func (repository *timedWorkerRepository) WithWrite(ctx context.Context, _ func(WriteScope) error) error {
	repository.traces <- telemetry.TraceID(ctx)
	telemetry.RecordTiming(ctx, telemetry.WriterPoolWait, time.Second)
	return nil
}
