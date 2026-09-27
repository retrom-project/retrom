package cleanupjobs

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func policyDeletionCandidate(state string, size int64) DeletionFile {
	return DeletionFile{
		ID: "candidate", Digest: strings.Repeat("a", 64), SizeBytes: size, HasCandidate: true,
		Candidate: DeletionCandidate{
			RetiredMS: 5, ScheduledMS: 100,
			Work: Work{
				ID: "deletion-job", Kind: "FILE_DELETE", Scope: Scope{Type: ScopeFile, ID: "candidate"},
				State: state, ExecutionNo: 1, Version: 1,
			},
		},
	}
}

func TestDeletionImmediateChecksAllCapacityBeforeWriting(t *testing.T) {
	t.Parallel()
	first, second := policyDeletionCandidate("QUEUED", math.MaxInt64), policyDeletionCandidate("FAILED", 1)
	second.ID = "second"
	second.Candidate.Work.Scope.ID = second.ID
	records := &deletionRepositoryFixture{facts: []DeletionFile{first, second}}
	result, err := newPolicyDeletion(t, records, nil).Immediate(t.Context(), "actor")
	if !errors.Is(err, ErrImmediateDeletionBytesOverflow) || result != (ImmediateDeletionResult{}) ||
		len(records.advanced) != 0 || len(records.audit) != 0 {
		t.Fatalf("capacity overflow changed cleanup: %+v/%v advances=%d", result, err, len(records.advanced))
	}
}

func TestDeletionStageRejectsNegativeClockWithoutQueueing(t *testing.T) {
	t.Parallel()
	records := &deletionRepositoryFixture{facts: []DeletionFile{{ID: "new", Digest: strings.Repeat("a", 64)}}}
	service := newPolicyDeletion(t, records, nil)
	service.now = func() time.Time { return time.UnixMilli(-1) }
	err := service.StageInScope(t.Context(), DeletionScope{Read: records, Write: records}, []string{"new"})
	if !errors.Is(err, ErrInputInvalid) || len(records.queued) != 0 {
		t.Fatalf("invalid clock produced work: %+v/%v", records.queued, err)
	}
}

func TestDeletionImmediatePreservesRunningAuthorityAndRenewsOnlyFailedInputs(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			before := policyDeletionCandidate(state, 12)
			records := &deletionRepositoryFixture{facts: []DeletionFile{before}}
			wakes := 0
			result, err := newPolicyDeletion(t, records, func() { wakes++ }).Immediate(t.Context(), "actor")
			if err != nil || result.FileCount != 1 || result.Bytes != 12 || len(records.advanced) != 1 ||
				len(records.audit) != 1 || wakes != 1 {
				t.Fatalf("immediate file deletion policy: %+v/%v", result, err)
			}
			change := records.advanced[0]
			if change.Before != before || change.ScheduledMS != 10 || (change.Retry != nil) != (state == "FAILED") {
				t.Fatalf("file deletion policy replaced authority: %+v", change)
			}
			if change.Retry != nil && (change.Retry.ExecutionNo != 2 || change.Retry.InputDigest == "") {
				t.Fatalf("file deletion manual retry missing new input: %+v", change.Retry)
			}
		})
	}
}
