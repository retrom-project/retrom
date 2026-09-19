package composition

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/capability/security/authn"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	jobsmodel "retrom/internal/model/jobs"
	es "retrom/internal/service/emulationstationimport"
	"retrom/internal/service/jobs"
)

type esCancellationStub struct {
	request es.JobCancellationRequest
	failure error
	ctx     context.Context
}

func (stub *esCancellationStub) CancelJob(ctx context.Context, request es.JobCancellationRequest) (es.JobCancellationResult, bool, error) {
	stub.ctx, stub.request = ctx, request
	return es.JobCancellationResult{JobID: request.JobID, State: "CANCEL_REQUESTED", ExecutionNo: 2, Version: 9}, true, stub.failure
}

func TestESJobCancellationPreservesActorContextVersionAndCauses(t *testing.T) {
	t.Parallel()
	storage := errors.New("database unavailable")
	for _, failure := range []error{nil, storage, emulationstationimportmodel.ErrNotCancellable, emulationstationimportmodel.ErrVersionConflict, emulationstationimportmodel.ErrNotFound} {
		stub := &esCancellationStub{failure: failure}
		ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
		result, pending, err := (emulationStationCancellation{source: stub}).CancelJob(ctx, jobs.DomainCancellation{JobID: "job", Kind: "SERVER_EMULATIONSTATION_SCAN", ScopeID: "plan", ExpectedVersion: 8, Reason: "Stop"})
		if stub.ctx != ctx || stub.request.ActorID != "actor" || stub.request.ExpectedVersion != 8 || stub.request.ScopeID != "plan" {
			t.Fatalf("command=%#v context=%v", stub.request, stub.ctx == ctx)
		}
		assertESCancellationResult(t, result, pending, err, failure, storage)
	}
}

func TestESJobCancellationRejectsMissingActor(t *testing.T) {
	t.Parallel()
	stub := &esCancellationStub{}
	_, _, err := (emulationStationCancellation{source: stub}).CancelJob(t.Context(), jobs.DomainCancellation{JobID: "job"})
	if !errors.Is(err, jobsmodel.ErrConflict) || stub.request.JobID != "" {
		t.Fatalf("actor missing err=%v command=%#v", err, stub.request)
	}
}

func assertESCancellationResult(t *testing.T, result jobs.Result, pending bool, err, failure, storage error) {
	t.Helper()
	if failure == nil {
		if err != nil || !pending || result.Version != 9 || result.ExecutionNo != 2 {
			t.Fatalf("result=%#v pending=%v err=%v", result, pending, err)
		}
		return
	}
	if !errors.Is(err, failure) || result.JobID != "" || pending {
		t.Fatalf("cause=%v result=%#v pending=%v", err, result, pending)
	}
	if errors.Is(err, jobsmodel.ErrConflict) == errors.Is(failure, storage) {
		t.Fatalf("wrong conflict classification: %v", err)
	}
}
