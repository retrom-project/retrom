package cleanupjobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type deletionRepositoryFixture struct {
	facts         []DeletionFile
	queued        []DeletionQueue
	advanced      []DeletionAdvance
	audit         []DeletionAudit
	fail          error
	calls, failAt int
}

func (r *deletionRepositoryFixture) WithDeletion(_ context.Context, run func(DeletionScope) error) error {
	queued, advanced, audits := len(r.queued), len(r.advanced), len(r.audit)
	err := run(DeletionScope{Read: r, Write: r})
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

func (r *deletionRepositoryFixture) Page(context.Context, string, int) ([]DeletionFile, error) {
	return nil, nil
}

func (r *deletionRepositoryFixture) Selected(context.Context, []string) ([]DeletionFile, error) {
	return r.facts, nil
}

func (r *deletionRepositoryFixture) Candidates(context.Context) ([]DeletionFile, error) {
	return r.facts, nil
}
func (r *deletionRepositoryFixture) Fence(context.Context, []DeletionFile) error { return nil }
func (r *deletionRepositoryFixture) Queue(_ context.Context, value DeletionQueue) error {
	r.queued = append(r.queued, value)
	return nil
}

func (r *deletionRepositoryFixture) Advance(_ context.Context, value DeletionAdvance) error {
	r.advanced = append(r.advanced, value)
	return nil
}

func (r *deletionRepositoryFixture) Audit(_ context.Context, value DeletionAudit) error {
	r.audit = append(r.audit, value)
	return nil
}

func newPolicyDeletion(t *testing.T, records *deletionRepositoryFixture, wake func()) *DeletionScheduler {
	t.Helper()
	service, err := NewDeletionScheduler(records, DeletionOptions{
		Now:  func() time.Time { return time.UnixMilli(10) },
		Wake: wake,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestDeletionStageSelectsOnlyNewUnprotectedCandidates(t *testing.T) {
	t.Parallel()
	records := &deletionRepositoryFixture{facts: []DeletionFile{
		{ID: "new", Digest: strings.Repeat("a", 64), SizeBytes: 5},
		{ID: "protected", Digest: strings.Repeat("b", 64), SizeBytes: 7, Retained: true},
		{ID: "existing", Digest: strings.Repeat("c", 64), SizeBytes: 9, HasCandidate: true},
	}}
	service := newPolicyDeletion(t, records, nil)
	err := service.StageInScope(t.Context(), DeletionScope{Read: records, Write: records}, []string{"new", "protected", "existing", "new"})
	if err != nil || len(records.queued) != 1 {
		t.Fatalf("candidate classification: %+v/%v", records.queued, err)
	}
	queued := records.queued[0]
	if queued.Before.ID != "new" || queued.AvailableMS != 10 || queued.Job.ID == "" || queued.Job.InputDigest == "" {
		t.Fatalf("invalid durable candidate: %+v", queued)
	}
}

func TestDeletionImmediateDoesNotReturnOrWakeAfterFailedCommit(t *testing.T) {
	t.Parallel()
	cause := errors.New("immediate cleanup commit unavailable")
	records := &deletionRepositoryFixture{fail: cause, failAt: 2}
	wakes := 0
	service := newPolicyDeletion(t, records, func() { wakes++ })
	result, err := service.Immediate(t.Context(), "actor")
	if !errors.Is(err, cause) || result != (ImmediateDeletionResult{}) || wakes != 0 || len(records.audit) != 0 {
		t.Fatalf("failed cleanup escaped transaction: %+v/%v wakes=%d", result, err, wakes)
	}
}

func TestDeletionIdentityFailureLeavesNoScheduledWork(t *testing.T) {
	t.Parallel()
	records := &deletionRepositoryFixture{facts: []DeletionFile{{ID: "new", Digest: strings.Repeat("a", 64), SizeBytes: 5}}}
	service := newPolicyDeletion(t, records, nil)
	cause := errors.New("file deletion entropy unavailable")
	service.newID = func() (string, error) { return "", cause }
	err := service.StageInScope(t.Context(), DeletionScope{Read: records, Write: records}, []string{"new"})
	if !errors.Is(err, cause) || len(records.queued) != 0 {
		t.Fatalf("failed identity produced file deletion work: %+v/%v", records.queued, err)
	}
}
