package payloadrelease

import (
	"context"
	"errors"
	model "retrom/internal/model/payloadrelease"
	"strings"
	"testing"
	"time"
)

type gcRepositoryFixture struct {
	facts         []model.GCBlob
	queued        []model.GCQueue
	advanced      []model.GCAdvance
	audit         []model.GCAudit
	fail          error
	calls, failAt int
}

func (r *gcRepositoryFixture) WithGC(_ context.Context, run func(model.GCScope) error) error {
	queued, advanced, audits := len(r.queued), len(r.advanced), len(r.audit)
	err := run(model.GCScope{Read: r, Write: r})
	if err == nil {
		r.calls++
		if r.calls == r.failAt {
			err = r.fail
		}
	}
	if err != nil {
		r.queued = r.queued[:queued]
		r.advanced = r.advanced[:advanced]
		r.audit = r.audit[:audits]
	}
	return err
}

func (r *gcRepositoryFixture) Page(context.Context, string, int) ([]model.GCBlob, error) {
	return nil, nil
}

func (r *gcRepositoryFixture) Selected(context.Context, []string) ([]model.GCBlob, error) {
	return r.facts, nil
}
func (r *gcRepositoryFixture) Candidates(context.Context) ([]model.GCBlob, error) {
	return r.facts, nil
}
func (r *gcRepositoryFixture) Fence(context.Context, []model.GCBlob) error { return nil }
func (r *gcRepositoryFixture) Queue(_ context.Context, value model.GCQueue) error {
	r.queued = append(r.queued, value)
	return nil
}

func (r *gcRepositoryFixture) Advance(_ context.Context, value model.GCAdvance) error {
	r.advanced = append(r.advanced, value)
	return nil
}
func (r *gcRepositoryFixture) Cancel(context.Context, model.GCCancellation) error { return nil }
func (r *gcRepositoryFixture) Audit(_ context.Context, value model.GCAudit) error {
	r.audit = append(r.audit, value)
	return nil
}

func newPolicyGC(t *testing.T, records *gcRepositoryFixture, wake func()) *GCScheduler {
	t.Helper()
	service, err := NewGCScheduler(records, GCOptions{
		Now:       func() time.Time { return time.UnixMilli(10) },
		Retention: 24 * time.Hour, Wake: wake,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestGCStageSelectsOnlyNewUnprotectedCandidates(t *testing.T) {
	t.Parallel()
	records := &gcRepositoryFixture{facts: []model.GCBlob{
		{ID: "new", Digest: strings.Repeat("a", 64), SizeBytes: 5},
		{ID: "protected", Digest: strings.Repeat("b", 64), SizeBytes: 7, Protected: true},
		{ID: "existing", Digest: strings.Repeat("c", 64), SizeBytes: 9, HasCandidate: true},
	}}
	service := newPolicyGC(t, records, nil)
	err := service.StageInScope(t.Context(), model.GCScope{Read: records, Write: records}, []string{"new", "protected", "existing", "new"})
	if err != nil || len(records.queued) != 1 {
		t.Fatalf("candidate classification: %+v/%v", records.queued, err)
	}
	queued := records.queued[0]
	if queued.Before.ID != "new" || queued.AvailableMS != 10+(24*time.Hour).Milliseconds() || queued.Job.ID == "" || queued.Job.InputDigest == "" {
		t.Fatalf("invalid durable candidate: %+v", queued)
	}
}

func TestGCImmediateDoesNotReturnOrWakeAfterFailedCommit(t *testing.T) {
	t.Parallel()
	cause := errors.New("immediate cleanup commit unavailable")
	records := &gcRepositoryFixture{fail: cause, failAt: 3}
	wakes := 0
	service := newPolicyGC(t, records, func() { wakes++ })
	result, err := service.Immediate(t.Context(), "actor")
	if !errors.Is(err, cause) || result != (model.ImmediateGCResult{}) || wakes != 0 || len(records.audit) != 0 {
		t.Fatalf("failed cleanup escaped transaction: %+v/%v wakes=%d", result, err, wakes)
	}
}

func TestGCIdentityFailureLeavesNoScheduledWork(t *testing.T) {
	t.Parallel()
	records := &gcRepositoryFixture{facts: []model.GCBlob{{ID: "new", Digest: strings.Repeat("a", 64), SizeBytes: 5}}}
	service := newPolicyGC(t, records, nil)
	cause := errors.New("GC entropy unavailable")
	service.newID = func() (string, error) { return "", cause }
	err := service.StageInScope(t.Context(), model.GCScope{Read: records, Write: records}, []string{"new"})
	if !errors.Is(err, cause) || len(records.queued) != 0 {
		t.Fatalf("failed identity produced GC work: %+v/%v", records.queued, err)
	}
}
