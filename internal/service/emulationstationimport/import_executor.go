package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
)

var ErrExecutionCancelled = errors.New("EmulationStation execution cancelled")

type ImportItems interface {
	Next(context.Context, Execution) (ExecutionItem, bool, error)
	Finish(context.Context, Execution, string, ItemOutcome) error
}
type ImportMaterials interface {
	Copy(context.Context, Execution, MaterialSource, VerifiedBlob) (string, error)
	Warning(context.Context, Execution, MaterialSource, string) error
	SetPhase(context.Context, Execution, string) error
}
type ImportSources interface {
	CopyFile(context.Context, Execution, ExecutionFile) (VerifiedBlob, error)
	CopyAsset(context.Context, Execution, ExecutionAsset) (VerifiedBlob, bool, error)
}
type ImportReviews interface {
	Resume(context.Context, Execution, ExecutionItem) (bool, error)
	Create(context.Context, Execution, ExecutionItem) error
}
type ImportControl interface {
	Observe(context.Context, Execution) (LeaseState, error)
	CloseCancelled(context.Context, Execution) (bool, error)
}
type ImportCompletion interface {
	Finish(context.Context, Execution) error
}
type FailureDiagnostics interface {
	Sanitize(error) string
	DatabaseCause(error) string
}
type ImportExecutorDependencies struct {
	Items       ImportItems
	Materials   ImportMaterials
	Sources     ImportSources
	Reviews     ImportReviews
	Control     ImportControl
	Completion  ImportCompletion
	Diagnostics FailureDiagnostics
}
type ImportExecutor struct{ dependencies ImportExecutorDependencies }

func NewImportExecutor(dependencies ImportExecutorDependencies) *ImportExecutor {
	return &ImportExecutor{dependencies: dependencies}
}

func (executor *ImportExecutor) Execute(ctx context.Context, unit Execution) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop EmulationStation import: %w", err)
		}
		cancelled, err := executor.dependencies.Control.CloseCancelled(ctx, unit)
		if err != nil {
			return fmt.Errorf("observe EmulationStation import cancellation: %w", err)
		}
		if cancelled {
			return nil
		}
		item, found, err := executor.dependencies.Items.Next(ctx, unit)
		if err != nil {
			return fmt.Errorf("claim EmulationStation import item: %w", err)
		}
		if !found {
			if err := executor.dependencies.Completion.Finish(ctx, unit); err != nil {
				return fmt.Errorf("complete EmulationStation import: %w", err)
			}
			return nil
		}
		if err := executor.Process(ctx, unit, item); err != nil {
			return err
		}
	}
}

func (executor *ImportExecutor) Process(ctx context.Context, unit Execution, item ExecutionItem) error {
	if err := executor.dependencies.Materials.SetPhase(ctx, unit, "COPYING_CONTENT"); err != nil {
		return fmt.Errorf("set EmulationStation copying phase: %w", err)
	}
	resumed, err := executor.dependencies.Reviews.Resume(ctx, unit, item)
	if err != nil {
		return fmt.Errorf("resume EmulationStation source handoff: %w", err)
	}
	if resumed {
		return nil
	}
	copied, err := executor.CopyFiles(ctx, unit, &item)
	if err != nil || !copied {
		return err
	}
	if err := executor.copyAssets(ctx, unit, &item); err != nil {
		return err
	}
	if err := executor.check(ctx, unit); err != nil {
		return err
	}
	if err := executor.dependencies.Reviews.Create(ctx, unit, item); err != nil {
		return fmt.Errorf("create EmulationStation review: %w", err)
	}
	return nil
}

func (executor *ImportExecutor) check(ctx context.Context, unit Execution) error {
	state, err := executor.dependencies.Control.Observe(ctx, unit)
	if err != nil {
		return fmt.Errorf("observe EmulationStation import owner: %w", err)
	}
	switch state {
	case LeaseActive:
		return nil
	case LeaseCancelled:
		return ErrExecutionCancelled
	case LeaseLost:
		return ErrVersionConflict
	case LeaseDeadline:
		return ErrExpired
	default:
		return ErrVersionConflict
	}
}

func importStopCause(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("stop EmulationStation source operation: %w", ctx.Err())
	}
	for _, stop := range []error{
		context.Canceled,
		context.DeadlineExceeded,
		ErrExecutionCancelled,
		ErrVersionConflict,
		ErrExpired,
	} {
		if errors.Is(err, stop) {
			return err
		}
	}
	return nil
}
