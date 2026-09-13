package emulationstationimport

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/capability/security/authn"
)

type processMemory struct {
	actor            string
	err              error
	queued           bool
	deleted, expired int
}

func (memory *processMemory) Create(_ context.Context, _ CreateRequest, actor string) (Summary, error) {
	memory.actor = actor
	return Summary{ID: "plan"}, memory.err
}

func (memory *processMemory) Start(_ context.Context, _ string, _ int64, actor string) (Summary, bool, error) {
	memory.actor = actor
	return Summary{ID: "plan"}, memory.queued, memory.err
}

func (memory *processMemory) Cancel(
	_ context.Context,
	_ string,
	_ int64,
	_ string,
	actor string,
) (Summary, bool, error) {
	memory.actor = actor
	return Summary{ID: "plan"}, true, memory.err
}

func (memory *processMemory) CancelJob(
	_ context.Context,
	request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	memory.actor = request.ActorID
	return JobCancellationResult{JobID: "job"}, true, memory.err
}

func (memory *processMemory) Retry(_ context.Context, _ string, _ int64, actor string) (Summary, error) {
	memory.actor = actor
	return Summary{ID: "plan"}, memory.err
}

func (memory *processMemory) Delete(_ context.Context, _ string, _ int64, actor string) error {
	memory.actor = actor
	memory.deleted++
	return memory.err
}
func (memory *processMemory) Expire(context.Context) error { memory.expired++; return memory.err }

type processWorker struct{ started, closed, signals int }

func (worker *processWorker) Start()  { worker.started++ }
func (worker *processWorker) Close()  { worker.closed++ }
func (worker *processWorker) Signal() { worker.signals++ }
func processService(memory *processMemory, worker *processWorker) *Service {
	return New(ServiceDependencies{Creation: memory, Starter: memory, Control: memory, Lifecycle: memory, Worker: worker})
}

func TestProcessServiceSignalsOnlyCommittedMutation(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "failed"}[failed], func(t *testing.T) {
			memory := &processMemory{}
			worker := &processWorker{}
			cause := errors.New("mutation failed")
			if failed {
				memory.err = cause
			}
			result, err := processService(memory, worker).Create(t.Context(), CreateRequest{}, "actor")
			if failed {
				if !errors.Is(err, cause) || result.ID != "" || worker.signals != 0 {
					t.Fatalf("result=%#v error=%v signals=%d", result, err, worker.signals)
				}
				return
			}
			if err != nil || result.ID != "plan" || worker.signals != 1 || memory.actor != "actor" {
				t.Fatalf("result=%#v error=%v worker=%#v", result, err, worker)
			}
		})
	}
}

func TestProcessServiceKeepsContextActorAndIdempotentStartSignal(t *testing.T) {
	memory := &processMemory{}
	worker := &processWorker{}
	service := processService(memory, worker)
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "context-actor", Role: "ADMIN"})
	if _, err := service.StartImport(ctx, "plan", 1); err != nil {
		t.Fatal(err)
	}
	if memory.actor != "context-actor" || worker.signals != 0 {
		t.Fatalf("actor=%s signals=%d", memory.actor, worker.signals)
	}
	memory.queued = true
	if _, err := service.StartImport(ctx, "plan", 1); err != nil {
		t.Fatal(err)
	}
	if worker.signals != 1 {
		t.Fatalf("signals=%d", worker.signals)
	}
	if err := service.Delete(ctx, "plan", 1); err != nil {
		t.Fatal(err)
	}
	if memory.actor != "context-actor" || memory.deleted != 1 {
		t.Fatalf("memory=%#v", memory)
	}
	service.Start()
	service.Close()
	if worker.started != 1 || worker.closed != 1 {
		t.Fatalf("worker=%#v", worker)
	}
}
