package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	model "retrom/internal/model/emulationstationimport"
)

var ErrExecutionCancelled = errors.New("EmulationStation execution cancelled")

type ImportItems interface {
	Next(context.Context, model.Execution) (model.ExecutionItem, bool, error)
	Finish(context.Context, model.Execution, string, model.ItemOutcome) error
}
type ImportMaterials interface {
	Copy(context.Context, model.Execution, model.MaterialSource, model.VerifiedBlob) (string, error)
	Warning(context.Context, model.Execution, model.MaterialSource, string) error
	SetPhase(context.Context, model.Execution, string) error
}
type ImportSources interface {
	CopyFile(context.Context, model.Execution, model.ExecutionFile) (model.VerifiedBlob, error)
	CopyAsset(context.Context, model.Execution, model.ExecutionAsset) (model.VerifiedBlob, bool, error)
}
type ImportReviews interface {
	Resume(context.Context, model.Execution, model.ExecutionItem) (bool, error)
	Create(context.Context, model.Execution, model.ExecutionItem) error
}
type ImportControl interface {
	Observe(context.Context, model.Execution) (model.LeaseState, error)
	CloseCancelled(context.Context, model.Execution) (bool, error)
}
type ImportCompletion interface {
	Finish(context.Context, model.Execution) error
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

func (executor *ImportExecutor) Execute(ctx context.Context, unit model.Execution) error {
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

func (executor *ImportExecutor) Process(ctx context.Context, unit model.Execution, item model.ExecutionItem) error {
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

func (executor *ImportExecutor) check(ctx context.Context, unit model.Execution) error {
	state, err := executor.dependencies.Control.Observe(ctx, unit)
	if err != nil {
		return fmt.Errorf("observe EmulationStation import owner: %w", err)
	}
	switch state {
	case model.LeaseActive:
		return nil
	case model.LeaseCancelled:
		return ErrExecutionCancelled
	case model.LeaseLost:
		return model.ErrVersionConflict
	case model.LeaseDeadline:
		return model.ErrExpired
	default:
		return model.ErrVersionConflict
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
		model.ErrVersionConflict,
		model.ErrExpired,
	} {
		if errors.Is(err, stop) {
			return err
		}
	}
	return nil
}
