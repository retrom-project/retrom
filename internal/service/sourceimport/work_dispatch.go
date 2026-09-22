package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrRootChanged     = errors.New("SERVER_IMPORT_ROOT_CHANGED")
	ErrRootUnavailable = errors.New("SERVER_IMPORT_ROOT_UNAVAILABLE")
)

type WorkSources interface {
	OpenScan(Work) (ScannerSource, error)
	OpenImport(Work) (ImportSources, error)
}
type (
	WorkFailureSettlement interface {
		Fail(context.Context, ExecutionIdentity, ExecutionFailure) error
	}
	ScanPublisher interface {
		Save(context.Context, ExecutionIdentity, ScanProjection) error
	}
	WorkDispatchDependencies struct {
		Sources     WorkSources
		Publication ScanPublisher
		Import      ImportExecutorDependencies
		Settlement  WorkFailureSettlement
		Report      func(error)
	}
)
type WorkDispatcher struct{ dependencies WorkDispatchDependencies }

func NewWorkDispatcher(dependencies WorkDispatchDependencies) *WorkDispatcher {
	return &WorkDispatcher{dependencies: dependencies}
}

func (dispatcher *WorkDispatcher) Execute(ctx context.Context, unit Work) {
	if ctx.Err() != nil {
		return
	}
	if unit.Kind == "IMPORT_SCAN" {
		dispatcher.scan(ctx, unit)
		return
	}
	source, err := dispatcher.dependencies.Sources.OpenImport(unit)
	if err != nil {
		dispatcher.fail(ctx, unit, err)
		return
	}
	dependencies := dispatcher.dependencies.Import
	dependencies.Sources = source
	if err := NewImportExecutor(dependencies).Execute(ctx, unit); err != nil {
		dispatcher.report(ctx, err)
	}
}

func (dispatcher *WorkDispatcher) scan(ctx context.Context, unit Work) {
	source, err := dispatcher.dependencies.Sources.OpenScan(unit)
	if err != nil {
		dispatcher.fail(ctx, unit, err)
		return
	}
	result, err := ScanOrganized(ctx, source, unit.Format, time.UnixMilli(unit.DeadlineAtMS).UTC().Year()+1)
	if err != nil {
		dispatcher.fail(ctx, unit, err)
		return
	}
	if err := dispatcher.dependencies.Publication.Save(ctx, unit.Identity(), result.Projection()); err != nil {
		dispatcher.failWith(ctx, unit, err, ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true})
	}
}

func (dispatcher *WorkDispatcher) fail(ctx context.Context, unit Work, cause error) {
	code := "INTERNAL_ERROR"
	for _, known := range []error{
		ErrRootChanged, ErrMetadataAbsent, ErrScanLimit, ErrSourceChanged,
		ErrMapping, ErrNoSelection, ErrExpired, ErrActive, ErrInvalid,
	} {
		if errors.Is(cause, known) {
			code = known.Error()
			break
		}
	}
	dispatcher.failWith(ctx, unit, cause, ExecutionFailure{Code: code, Retryable: errors.Is(cause, ErrRootUnavailable)})
}

func (dispatcher *WorkDispatcher) failWith(ctx context.Context, unit Work, cause error, failure ExecutionFailure) {
	if importStopCause(ctx, cause) != nil {
		return
	}
	if err := dispatcher.dependencies.Settlement.Fail(ctx, unit.Identity(), failure); err != nil {
		dispatcher.report(ctx, fmt.Errorf("settle Source work: %w", errors.Join(cause, err)))
	}
}

func (dispatcher *WorkDispatcher) report(ctx context.Context, err error) {
	if ctx.Err() == nil && dispatcher.dependencies.Report != nil {
		dispatcher.dependencies.Report(err)
	}
}
