package payloadrelease

import (
	"errors"
	"math"
	model "retrom/internal/model/payloadrelease"
	"strings"
	"testing"
	"time"
)

func policyGCCandidate(state string, size int64) model.GCBlob {
	return model.GCBlob{
		ID: "candidate", Digest: strings.Repeat("a", 64), SizeBytes: size, HasCandidate: true,
		Candidate: model.GCCandidate{
			FirstUnreferencedMS: 5, ScheduledMS: 100,
			Work: model.Work{
				ID: "gc-job", Kind: "BLOB_GC", Scope: model.Scope{Type: model.ScopeBlob, ID: "candidate"},
				State: state, ExecutionNo: 1, Version: 1,
			},
		},
	}
}

func TestGCImmediateChecksAllCapacityBeforeWriting(t *testing.T) {
	t.Parallel()
	first, second := policyGCCandidate("QUEUED", math.MaxInt64), policyGCCandidate("FAILED", 1)
	second.ID = "second"
	second.Candidate.Work.Scope.ID = second.ID
	records := &gcRepositoryFixture{facts: []model.GCBlob{first, second}}
	result, err := newPolicyGC(t, records, nil).Immediate(t.Context(), "actor")
	if !errors.Is(err, model.ErrImmediateGCBytesOverflow) || result != (model.ImmediateGCResult{}) ||
		len(records.advanced) != 0 || len(records.audit) != 0 {
		t.Fatalf("capacity overflow changed cleanup: %+v/%v advances=%d", result, err, len(records.advanced))
	}
}

func TestGCStageRejectsOverflowingRetentionWithoutQueueing(t *testing.T) {
	t.Parallel()
	records := &gcRepositoryFixture{facts: []model.GCBlob{{ID: "new", Digest: strings.Repeat("a", 64)}}}
	service := newPolicyGC(t, records, nil)
	service.now = func() time.Time { return time.UnixMilli(math.MaxInt64) }
	err := service.StageInScope(t.Context(), model.GCScope{Read: records, Write: records}, []string{"new"})
	if !errors.Is(err, model.ErrGCRetentionInvalid) || len(records.queued) != 0 {
		t.Fatalf("overflowing retention produced work: %+v/%v", records.queued, err)
	}
}

func TestGCImmediatePreservesRunningAuthorityAndRenewsOnlyFailedInputs(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			before := policyGCCandidate(state, 12)
			records := &gcRepositoryFixture{facts: []model.GCBlob{before}}
			wakes := 0
			result, err := newPolicyGC(t, records, func() { wakes++ }).Immediate(t.Context(), "actor")
			if err != nil || result.BlobCount != 1 || result.Bytes != 12 || len(records.advanced) != 1 ||
				len(records.audit) != 1 || wakes != 1 {
				t.Fatalf("immediate GC policy: %+v/%v", result, err)
			}
			change := records.advanced[0]
			if change.Before != before || change.ScheduledMS != 10 || (change.Retry != nil) != (state == "FAILED") {
				t.Fatalf("GC policy replaced authority: %+v", change)
			}
			if change.Retry != nil && (change.Retry.ExecutionNo != 2 || change.Retry.InputDigest == "") {
				t.Fatalf("GC manual retry missing new input: %+v", change.Retry)
			}
		})
	}
}
