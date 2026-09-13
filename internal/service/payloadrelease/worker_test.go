package payloadrelease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerRepositoryFixture struct {
	work      Work
	changes   []WorkChange
	commitErr error
}

func (r *workerRepositoryFixture) WithWorker(_ context.Context, run func(WorkerScope) error) error {
	before, changes := r.work, len(r.changes)
	err := run(WorkerScope{Read: r, Write: r, Owners: r})
	if err == nil {
		err = r.commitErr
	}
	if err != nil {
		r.work = before
		r.changes = r.changes[:changes]
	}
	return err
}

func (r *workerRepositoryFixture) Next(context.Context, int64) (Work, bool, error) {
	return r.work, r.work.State == "QUEUED", nil
}

func (r *workerRepositoryFixture) Current(_ context.Context, id string) (Work, bool, error) {
	return r.work, r.work.ID == id, nil
}

func (r *workerRepositoryFixture) Interrupted(context.Context, int64, int) ([]Work, error) {
	return []Work{r.work}, nil
}

func (r *workerRepositoryFixture) Change(_ context.Context, c WorkChange) error {
	if c.Before != r.work {
		return ErrExecutionLost
	}
	r.work = c.After
	r.changes = append(r.changes, c)
	return nil
}

func (r *workerRepositoryFixture) Fence(_ context.Context, work Work) error {
	if r.work != work {
		return ErrExecutionLost
	}
	return nil
}

func workerPolicyFixture() (*Worker, *workerRepositoryFixture) {
	r := &workerRepositoryFixture{work: Work{
		ID: "release", Kind: "PAYLOAD_RELEASE", Scope: Scope{Type: ScopeGame, ID: "game"},
		State: "QUEUED", MaxAttempts: 4, ExecutionNo: 1, Version: 1, AvailableMS: 10,
	}}
	w := NewWorker(r, nil, WorkerOptions{Now: func() time.Time { return time.UnixMilli(10) }, NewID: func() (string, error) { return "worker-one", nil }})
	return w, r
}

func TestWorkerClaimCommitsAuthorityAndRetainsOriginalBudget(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	r.work.Started = WorkTime{Value: 1, Set: true}
	r.work.Deadline = WorkTime{Value: 1000, Set: true}
	unit, found, err := w.Claim(t.Context())
	if err != nil || !found || unit != r.work || unit.WorkerID != "worker-one" || unit.Attempt != 1 || unit.Version != 2 ||
		unit.Started.Value != 1 || unit.Deadline.Value != 1000 || unit.Lease.Value != 1000 {
		t.Fatalf("claim authority or original budget lost: %+v/%t/%v", unit, found, err)
	}
	if len(r.changes) != 1 || r.changes[0].EventType != "STARTED" {
		t.Fatalf("claim event not atomic: %+v", r.changes)
	}
}

func TestWorkerClaimDoesNotExposeRolledBackAuthority(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	before := r.work
	cause := errors.New("commit unavailable")
	r.commitErr = cause
	unit, found, err := w.Claim(t.Context())
	if !errors.Is(err, cause) || found || unit.ID != "" || r.work != before || len(r.changes) != 0 {
		t.Fatalf("rolled back claim escaped: %+v/%t/%v", unit, found, err)
	}
}

func TestWorkerClaimPreservesIdentityFailure(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	before := r.work
	cause := errors.New("entropy failed")
	w.newID = func() (string, error) { return "", cause }
	unit, found, err := w.Claim(t.Context())
	if !errors.Is(err, cause) || found || unit.ID != "" || r.work != before {
		t.Fatalf("identity failure escaped: %+v/%t/%v", unit, found, err)
	}
}

func TestWorkerFinishRejectsReplacedExecution(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	unit, _, err := w.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r.work.WorkerID = "replacement"
	r.work.Version++
	before := r.work
	if err := w.Finish(t.Context(), unit, nil); !errors.Is(err, ErrExecutionLost) || r.work != before || len(r.changes) != 1 {
		t.Fatalf("stale completion accepted: %v %+v", err, r.work)
	}
}

func TestWorkerRecoveryPreservesLiveLeaseAndTerminalizesExhaustion(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	_, _, err := w.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before := r.work
	if err := w.Recover(t.Context()); err != nil || r.work != before {
		t.Fatalf("live lease stolen: %v", err)
	}
	r.work.Lease = WorkTime{Set: true, Value: 9}
	r.work.Attempt = 4
	if err := w.Recover(t.Context()); err != nil || r.work.State != "FAILED" || r.work.Attempt != 4 || r.work.Deadline != before.Deadline {
		t.Fatalf("exhausted recovery extended budget: %+v/%v", r.work, err)
	}
}

func (r *workerRepositoryFixture) Owner(_ context.Context, scope Scope) (Owner, error) {
	return Owner{Scope: scope, Version: 2, PayloadState: "RELEASING", ReleaseJobID: r.work.ID}, nil
}

type workerEffectFailure struct{}

func (workerEffectFailure) Error() string { return "effect failed while deadline expired" }
func (workerEffectFailure) Code() string  { return "PAYLOAD_RELEASE_DATABASE_FAILED" }
func TestWorkerOwnDeadlineControlsFailureSettlement(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	unit, found, err := w.Claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim: %t/%v", found, err)
	}
	err = w.Finish(t.Context(), unit, errors.Join(workerEffectFailure{}, ErrExecutionTimeout))
	last := r.changes[len(r.changes)-1]
	if err != nil || r.work.State != "FAILED" || last.ErrorCode != ErrExecutionTimeout.Error() {
		t.Fatalf("effect error masked execution deadline: state=%s code=%s error=%v", r.work.State, last.ErrorCode, err)
	}
}
