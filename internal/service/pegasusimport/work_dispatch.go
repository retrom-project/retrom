package pegasusimport

import (
	"context"
	"errors"
	"fmt"

	model "retrom/internal/model/pegasusimport"
)

var (
	ErrRootChanged     = errors.New("SERVER_IMPORT_ROOT_CHANGED")
	ErrRootUnavailable = errors.New("SERVER_IMPORT_ROOT_UNAVAILABLE")
)

type WorkSources interface {
	OpenScan(model.Work) (ScannerSource, error)
	OpenImport(model.Work) (ImportSources, error)
}
type (
	WorkFailureSettlement interface {
		Fail(context.Context, model.ExecutionIdentity, model.ExecutionFailure) error
	}
	ScanPublisher interface {
		Save(context.Context, model.ExecutionIdentity, model.ScanProjection) error
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

func (dispatcher *WorkDispatcher) Execute(ctx context.Context, unit model.Work) {
	if ctx.Err() != nil {
		return
	}
	if unit.Kind == "SERVER_PEGASUS_SCAN" {
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

func (dispatcher *WorkDispatcher) scan(ctx context.Context, unit model.Work) {
	source, err := dispatcher.dependencies.Sources.OpenScan(unit)
	if err != nil {
		dispatcher.fail(ctx, unit, err)
		return
	}
	result, err := NewScanner(source).Scan(ctx)
	if err != nil {
		dispatcher.fail(ctx, unit, err)
		return
	}
	if err := dispatcher.dependencies.Publication.Save(ctx, unit.Identity(), result.Projection()); err != nil {
		dispatcher.failWith(ctx, unit, err, model.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true})
	}
}

func (dispatcher *WorkDispatcher) fail(ctx context.Context, unit model.Work, cause error) {
	code := "INTERNAL_ERROR"
	for _, known := range []error{
		ErrRootChanged, model.ErrMetadataAbsent, model.ErrScanLimit, model.ErrSourceChanged,
		model.ErrMapping, model.ErrNoSelection, model.ErrExpired, model.ErrActive, model.ErrInvalid,
	} {
		if errors.Is(cause, known) {
			code = known.Error()
			break
		}
	}
	dispatcher.failWith(
		ctx,
		unit,
		cause,
		model.ExecutionFailure{Code: code, Retryable: errors.Is(cause, ErrRootUnavailable)},
	)
}

func (dispatcher *WorkDispatcher) failWith(
	ctx context.Context,
	unit model.Work,
	cause error,
	failure model.ExecutionFailure,
) {
	if importStopCause(ctx, cause) != nil {
		return
	}
	if err := dispatcher.dependencies.Settlement.Fail(ctx, unit.Identity(), failure); err != nil {
		dispatcher.report(ctx, fmt.Errorf("settle Pegasus work: %w", errors.Join(cause, err)))
	}
}

func (dispatcher *WorkDispatcher) report(ctx context.Context, err error) {
	if ctx.Err() == nil && dispatcher.dependencies.Report != nil {
		dispatcher.dependencies.Report(err)
	}
}
