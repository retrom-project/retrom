package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	model "retrom/internal/model/payloadrelease"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type synchronizedWork struct {
	mutex   sync.Mutex
	records *workerRepositoryFixture
}

func (r *synchronizedWork) WithWorker(ctx context.Context, run func(model.WorkerScope) error) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.records.WithWorker(ctx, run)
}

func (r *synchronizedWork) snapshot() model.Work {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.records.work
}

func (r *synchronizedWork) replaceWorker() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.records.work.WorkerID = "replacement"
	r.records.work.Version++
}

type workerExecuteFunc func(context.Context, model.Execution) error

func (run workerExecuteFunc) Execute(ctx context.Context, execution model.Execution) error {
	return run(ctx, execution)
}

type workerRunResult struct {
	did bool
	err error
}

func concurrentWorker(t *testing.T, limit time.Duration) (*Worker, *synchronizedWork, <-chan model.Execution) {
	t.Helper()
	w, records := workerPolicyFixture()
	now := time.Now().UnixMilli()
	input := `{"schemaVersion":1,"kind":"PAYLOAD_RELEASE","scope":{"type":"GAME","id":"game"},` +
		`"executionId":"01992875-0000-7000-8000-000000000001","inputs":{"scopeVersion":2,"reason":"GAME_DELETED"}}`
	digest := sha256.Sum256([]byte(input))
	records.work.InputJSON = input
	records.work.InputDigest = hex.EncodeToString(digest[:])
	records.work.InputFound = true
	records.work.Started = model.WorkTime{Set: true, Value: now}
	records.work.Deadline = model.WorkTime{Set: true, Value: now + limit.Milliseconds()}
	repository := &synchronizedWork{records: records}
	w.repository = repository
	w.now = time.Now
	entered := make(chan model.Execution, 1)
	w.executor = workerExecuteFunc(func(ctx context.Context, execution model.Execution) error {
		entered <- execution
		<-ctx.Done()
		return ctx.Err()
	})
	t.Cleanup(w.Close)
	return w, repository, entered
}

func runWorker(ctx context.Context, w *Worker) <-chan workerRunResult {
	result := make(chan workerRunResult, 1)
	go func() { did, err := w.RunOnce(ctx); result <- workerRunResult{did: did, err: err} }()
	return result
}

func TestWorkerCloseCancelsAndJoinsDirectRunAndRejectsRestart(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w, r, entered := concurrentWorker(t, ExecutionTimeout)
		result := runWorker(t.Context(), w)
		unit := <-entered
		w.Close()
		outcome := <-result
		if !outcome.did || !errors.Is(outcome.err, model.ErrWorkerClosed) {
			t.Fatalf("close did not retain cause: %+v", outcome)
		}
		after := r.snapshot()
		if after.State != "QUEUED" || after.Started != unit.Work.Started || after.Deadline != unit.Work.Deadline {
			t.Fatalf("close lost durable resumable budget: %+v", after)
		}
		w.Start()
		if did, err := w.RunOnce(t.Context()); did || !errors.Is(err, model.ErrWorkerClosed) || r.snapshot() != after {
			t.Fatalf("closed worker restarted: %t/%v", did, err)
		}
	})
}

func TestWorkerShortCallerDeadlineDoesNotConsumeExecutionBudget(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w, r, entered := concurrentWorker(t, ExecutionTimeout)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		result := runWorker(ctx, w)
		unit := <-entered
		outcome := <-result
		after := r.snapshot()
		if !outcome.did || !errors.Is(outcome.err, context.DeadlineExceeded) || errors.Is(outcome.err, model.ErrExecutionTimeout) ||
			after.State != "QUEUED" || after.Started != unit.Work.Started || after.Deadline != unit.Work.Deadline {
			t.Fatalf("caller timeout replaced original budget: %+v %+v", outcome, after)
		}
	})
}

func TestWorkerOriginalDeadlineTerminatesTheExecution(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w, r, entered := concurrentWorker(t, 100*time.Millisecond)
		result := runWorker(t.Context(), w)
		unit := <-entered
		outcome := <-result
		after := r.snapshot()
		if !outcome.did || !errors.Is(outcome.err, model.ErrExecutionTimeout) || after.State != "FAILED" ||
			after.Attempt != 1 || after.Deadline != unit.Work.Deadline {
			t.Fatalf("execution timeout extended: %+v %+v", outcome, after)
		}
	})
}

func TestWorkerMonitorCancelsReplacedOwnerWithoutSettlingReplacement(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w, r, entered := concurrentWorker(t, ExecutionTimeout)
		result := runWorker(t.Context(), w)
		<-entered
		r.replaceWorker()
		before := r.snapshot()
		outcome := <-result
		if !outcome.did || !errors.Is(outcome.err, model.ErrExecutionLost) || r.snapshot() != before {
			t.Fatalf("monitor failed to stop stale owner: %+v %+v", outcome, r.snapshot())
		}
	})
}

func TestWorkerHeartbeatKeepsLongExecutionOwnedUntilClose(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w, r, entered := concurrentWorker(t, ExecutionTimeout)
		result := runWorker(t.Context(), w)
		unit := <-entered
		time.Sleep(16 * time.Second)
		synctest.Wait()
		after := r.snapshot()
		if after.Lease.Value <= unit.Work.Lease.Value || after.Deadline != unit.Work.Deadline || after.WorkerID != unit.Work.WorkerID {
			t.Fatalf("heartbeat did not retain original execution: %+v %+v", unit.Work, after)
		}
		w.Close()
		outcome := <-result
		if !errors.Is(outcome.err, model.ErrWorkerClosed) {
			t.Fatalf("long worker outlived close: %+v", outcome)
		}
	})
}
