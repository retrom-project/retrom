package sourceimport

import (
	"context"
	"errors"
	"testing"
)

type dispatchSources struct {
	failure error
	opened  []string
}

func (fake *dispatchSources) OpenScan(Work) (ScannerSource, error) {
	fake.opened = append(fake.opened, "scan")
	return nil, fake.failure
}

func (fake *dispatchSources) OpenImport(Work) (ImportSources, error) {
	fake.opened = append(fake.opened, "import")
	return nil, fake.failure
}

type dispatchSettlement struct {
	calls   []ExecutionFailure
	failure error
}

func (fake *dispatchSettlement) Fail(_ context.Context, _ ExecutionIdentity, failure ExecutionFailure) error {
	fake.calls = append(fake.calls, failure)
	return fake.failure
}

func TestWorkDispatcherRejectsChangedRootBeforeReadingSource(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"IMPORT_SCAN", "IMPORT_RECEIVE"} {
		t.Run(kind, func(t *testing.T) {
			source := &dispatchSources{failure: ErrRootChanged}
			settle := &dispatchSettlement{}
			var reported error
			worker := NewWorkDispatcher(WorkDispatchDependencies{Sources: source, Settlement: settle, Report: func(err error) { reported = err }})
			worker.Execute(t.Context(), Work{Kind: kind})
			if len(settle.calls) != 1 || settle.calls[0].Code != "SERVER_IMPORT_ROOT_CHANGED" || settle.calls[0].Retryable || reported != nil {
				t.Fatalf("changed root settlement=%+v error=%v", settle.calls, reported)
			}
		})
	}
}

func TestWorkDispatcherPreservesFailedSettlementAndStopsCancelledWork(t *testing.T) {
	t.Parallel()
	source := &dispatchSources{failure: ErrRootUnavailable}
	failure := errors.New("settlement unavailable")
	settle := &dispatchSettlement{failure: failure}
	var reported error
	worker := NewWorkDispatcher(WorkDispatchDependencies{Sources: source, Settlement: settle, Report: func(err error) { reported = err }})
	worker.Execute(t.Context(), Work{Kind: "IMPORT_SCAN"})
	if !errors.Is(reported, failure) || !errors.Is(reported, ErrRootUnavailable) || len(settle.calls) != 1 || !settle.calls[0].Retryable {
		t.Fatalf("failure=%v settlement=%+v", reported, settle.calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	worker.Execute(ctx, Work{Kind: "IMPORT_SCAN"})
	if len(source.opened) != 1 || len(settle.calls) != 1 {
		t.Fatal("cancelled execution touched source or settlement")
	}
}
