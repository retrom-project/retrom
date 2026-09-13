package libraryimport

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type managedImportFixture struct {
	work                         ImportWork
	claimed                      atomic.Bool
	prepares, progress, finishes atomic.Int64
	entered                      chan struct{}
	failEntered                  chan struct{}
	releaseFail                  chan struct{}
	releaseOnce                  sync.Once
	failureMu                    sync.Mutex
	failure                      error
	closedFailureContext         bool
}

func newManagedImportFixture() *managedImportFixture {
	now := time.Now().UnixMilli()
	return &managedImportFixture{
		work: ImportWork{
			Execution: QueuedImportExecution{
				ImportID:    "import",
				JobID:       "job",
				WorkerID:    "worker",
				ExecutionNo: 2,
				Attempt:     1,
				StartedAtMS: now,
				DeadlineMS:  now + 60000,
			},
		},
		entered:     make(chan struct{}),
		failEntered: make(chan struct{}),
		releaseFail: make(chan struct{}),
	}
}

func (*managedImportFixture) Queued(context.Context) ([]string, error) { return []string{"job"}, nil }

func (fixture *managedImportFixture) Claim(context.Context, string) (ImportWork, bool, error) {
	if fixture.claimed.Swap(true) {
		return ImportWork{}, false, nil
	}
	return fixture.work, true, nil
}
func (*managedImportFixture) Recover(context.Context) error { return nil }
func (*managedImportFixture) Renew(context.Context, QueuedImportExecution) (bool, error) {
	return false, nil
}

func (fixture *managedImportFixture) Progress(context.Context, QueuedImportExecution, int) error {
	fixture.progress.Add(1)
	return nil
}

func (fixture *managedImportFixture) Prepare(ctx context.Context, _ ImportRequest) (PreparedImport, error) {
	fixture.prepares.Add(1)
	close(fixture.entered)
	<-ctx.Done()
	return PreparedImport{}, ctx.Err()
}

func (*managedImportFixture) CommitPrepared(
	context.Context,
	PreparedImport,
	ImportCreationOptions,
) (ImportCreationResult, error) {
	return ImportCreationResult{}, errors.New("unexpected commit after blocked preparation")
}

func (fixture *managedImportFixture) Fail(ctx context.Context, _ QueuedImportExecution, cause error) error {
	fixture.finishes.Add(1)
	fixture.failureMu.Lock()
	fixture.failure = cause
	fixture.closedFailureContext = ctx.Err() != nil
	fixture.failureMu.Unlock()
	close(fixture.failEntered)
	<-fixture.releaseFail
	return nil
}

func (fixture *managedImportFixture) release() {
	fixture.releaseOnce.Do(func() { close(fixture.releaseFail) })
}

func (fixture *managedImportFixture) worker() *ImportWorker {
	return NewImportWorker(
		ImportWorkerDependencies{
			Queue:       fixture,
			Control:     fixture,
			Recovery:    fixture,
			Preparation: fixture,
			Creations:   fixture,
		},
		ImportWorkerSettings{},
	)
}

func awaitImportSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("managed import checkpoint was not reached")
	}
}

func TestImportWorkerCloseCancelsPreparationAndJoinsSettlement(t *testing.T) {
	fixture := newManagedImportFixture()
	worker := fixture.worker()
	t.Cleanup(func() { fixture.release(); worker.Close() })
	worker.Start()
	awaitImportSignal(t, fixture.entered)
	closed := make(chan struct{})
	go func() { defer close(closed); worker.Close() }()
	awaitImportSignal(t, fixture.failEntered)
	select {
	case <-closed:
		t.Fatal("Close returned before settlement finished")
	default:
	}
	fixture.release()
	awaitImportSignal(t, closed)
	fixture.failureMu.Lock()
	failure, closedContext := fixture.failure, fixture.closedFailureContext
	fixture.failureMu.Unlock()
	if !errors.Is(failure, ErrImportWorkerClosed) || closedContext || fixture.prepares.Load() != 1 ||
		fixture.progress.Load() != 0 ||
		fixture.finishes.Load() != 1 {
		t.Fatalf(
			"shutdown cause=%v canceledSettlement=%t prepare=%d progress=%d finish=%d",
			failure,
			closedContext,
			fixture.prepares.Load(),
			fixture.progress.Load(),
			fixture.finishes.Load(),
		)
	}
	worker.NotifyImportGroup(t.Context(), "job")
	if err := worker.Run(t.Context(), fixture.work); !errors.Is(err, ErrImportWorkerClosed) {
		t.Fatalf("closed Run error=%v", err)
	}
	if fixture.finishes.Load() != 1 {
		t.Fatal("work invoked after Close wrote a new settlement")
	}
}

func TestImportWorkerConcurrentNotificationsRegisterOnePreparation(t *testing.T) {
	fixture := newManagedImportFixture()
	worker := fixture.worker()
	t.Cleanup(func() { fixture.release(); worker.Close() })
	var notify sync.WaitGroup
	for range 20 {
		notify.Go(func() { worker.NotifyImportGroup(context.Background(), "job") })
	}
	notify.Wait()
	awaitImportSignal(t, fixture.entered)
	closed := make(chan struct{})
	go func() { defer close(closed); worker.Close() }()
	awaitImportSignal(t, fixture.failEntered)
	fixture.release()
	awaitImportSignal(t, closed)
	if fixture.prepares.Load() != 1 || fixture.finishes.Load() != 1 {
		t.Fatalf("duplicate work prepare=%d finish=%d", fixture.prepares.Load(), fixture.finishes.Load())
	}
}

type importClaimBarrier struct {
	*managedImportFixture
	claimEntered, claimCancelled, releaseClaim chan struct{}
}

func (gate importClaimBarrier) Claim(ctx context.Context, _ string) (ImportWork, bool, error) {
	close(gate.claimEntered)
	<-ctx.Done()
	close(gate.claimCancelled)
	<-gate.releaseClaim
	return gate.work, true, nil
}

func TestImportWorkerCloseJoinsClaimedWorkBeforeRunRegistration(t *testing.T) {
	fixture := newManagedImportFixture()
	gate := importClaimBarrier{
		managedImportFixture: fixture,
		claimEntered:         make(chan struct{}),
		claimCancelled:       make(chan struct{}),
		releaseClaim:         make(chan struct{}),
	}
	worker := fixture.worker()
	worker.dependencies.Queue = gate
	var releaseClaim sync.Once
	unblock := func() { releaseClaim.Do(func() { close(gate.releaseClaim) }) }
	t.Cleanup(func() { unblock(); fixture.release(); worker.Close() })
	worker.Start()
	awaitImportSignal(t, gate.claimEntered)
	closed := make(chan struct{})
	go func() { defer close(closed); worker.Close() }()
	awaitImportSignal(t, gate.claimCancelled)
	select {
	case <-closed:
		t.Fatal("Close returned while committed claim was still being handed off")
	default:
	}
	unblock()
	awaitImportSignal(t, fixture.failEntered)
	fixture.release()
	awaitImportSignal(t, closed)
	if fixture.prepares.Load() != 0 || fixture.finishes.Load() != 1 {
		t.Fatalf("claim handoff: prepares=%d finishes=%d", fixture.prepares.Load(), fixture.finishes.Load())
	}
}
