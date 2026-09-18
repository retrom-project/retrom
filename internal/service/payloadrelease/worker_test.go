package payloadrelease

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/payloadrelease"
)

type workerRepositoryFixture struct {
	work      model.Work
	changes   []model.WorkChange
	commitErr error
}

func (r *workerRepositoryFixture) LoadNextWork(_ context.Context, _ int64) (model.Work, bool, error) {
	return r.work, r.work.State == "QUEUED", nil
}

func (r *workerRepositoryFixture) LoadCurrentWork(_ context.Context, id string) (model.Work, bool, error) {
	return r.work, r.work.ID == id, nil
}

func (r *workerRepositoryFixture) LoadInterruptedWork(_ context.Context, _ int64, _ int) ([]model.Work, error) {
	return []model.Work{r.work}, nil
}

func (r *workerRepositoryFixture) LoadWorkOwner(_ context.Context, scope model.Scope) (model.Owner, error) {
	return model.Owner{Scope: scope, Version: 2, PayloadState: "RELEASING", ReleaseJobID: r.work.ID}, nil
}

func (r *workerRepositoryFixture) CommitWorkChange(_ context.Context, c model.WorkChange) error {
	if c.Before != r.work {
		return model.ErrExecutionLost
	}
	if r.commitErr != nil {
		return r.commitErr
	}
	r.work = c.After
	r.changes = append(r.changes, c)
	return nil
}

func (r *workerRepositoryFixture) CommitWorkFence(_ context.Context, work model.Work) error {
	if r.work != work {
		return model.ErrExecutionLost
	}
	return nil
}

func workerPolicyFixture() (*Worker, *workerRepositoryFixture) {
	r := &workerRepositoryFixture{work: model.Work{
		ID: "release", Kind: "PAYLOAD_RELEASE", Scope: model.Scope{Type: model.ScopeGame, ID: "game"},
		State: "QUEUED", MaxAttempts: 4, ExecutionNo: 1, Version: 1, AvailableMS: 10,
	}}
	w := NewWorker(r, nil, WorkerOptions{Now: func() time.Time { return time.UnixMilli(10) }, NewID: func() (string, error) { return "worker-one", nil }})
	return w, r
}

func TestWorkerClaimCommitsAuthorityAndRetainsOriginalBudget(t *testing.T) {
	t.Parallel()
	w, r := workerPolicyFixture()
	r.work.Started = model.WorkTime{Value: 1, Set: true}
	r.work.Deadline = model.WorkTime{Value: 1000, Set: true}
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
	if err := w.Finish(t.Context(), unit, nil); !errors.Is(err, model.ErrExecutionLost) || r.work != before || len(r.changes) != 1 {
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
	r.work.Lease = model.WorkTime{Set: true, Value: 9}
	r.work.Attempt = 4
	if err := w.Recover(t.Context()); err != nil || r.work.State != "FAILED" || r.work.Attempt != 4 || r.work.Deadline != before.Deadline {
		t.Fatalf("exhausted recovery extended budget: %+v/%v", r.work, err)
	}
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
	err = w.Finish(t.Context(), unit, errors.Join(workerEffectFailure{}, model.ErrExecutionTimeout))
	last := r.changes[len(r.changes)-1]
	if err != nil || r.work.State != "FAILED" || last.ErrorCode != model.ErrExecutionTimeout.Error() {
		t.Fatalf("effect error masked execution deadline: state=%s code=%s error=%v", r.work.State, last.ErrorCode, err)
	}
}
