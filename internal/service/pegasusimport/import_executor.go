package pegasusimport

import (
	"context"
	"errors"
	"fmt"

	library "retrom/internal/service/libraryimport"
)

type (
	ImportItems interface {
		Next(context.Context, ExecutionIdentity) (ExecutionItem, bool, error)
		Finish(context.Context, ExecutionIdentity, string, ItemOutcome) error
	}
	ImportMaterials interface {
		Copy(context.Context, ExecutionIdentity, MaterialSource, VerifiedBlob) (string, error)
		Warning(context.Context, ExecutionIdentity, MaterialSource, string) error
		SetPhase(context.Context, ExecutionIdentity, string) error
		Cancelled(context.Context, ExecutionIdentity) (bool, error)
	}
	ImportSources interface {
		CopyFile(context.Context, Work, ExecutionFile) (VerifiedBlob, error)
		CopyAsset(context.Context, Work, ExecutionAsset) (VerifiedBlob, bool, error)
	}
	ImportReviews interface {
		Resume(context.Context, Work, ExecutionItem) (bool, error)
		Create(context.Context, Work, ExecutionItem, []library.ServerSourceFile) error
	}
	ImportCompanions interface {
		Find(context.Context, ExecutionIdentity, string) ([]CompanionCandidate, error)
		Record(context.Context, ExecutionIdentity, string, CompanionCandidate, VerifiedBlob) (string, error)
	}
	ImportSettlement interface {
		Cancelled(context.Context, ExecutionIdentity) (bool, error)
		Fail(context.Context, ExecutionIdentity, ExecutionFailure) error
	}
	ImportCompletion interface {
		Finish(context.Context, ExecutionIdentity) error
	}
	FailureDiagnostics interface {
		Sanitize(error) string
		DatabaseCause(error) string
	}
	ImportExecutorDependencies struct {
		Items       ImportItems
		Materials   ImportMaterials
		Sources     ImportSources
		Reviews     ImportReviews
		Companions  ImportCompanions
		Settlement  ImportSettlement
		Completion  ImportCompletion
		Diagnostics FailureDiagnostics
	}
	ImportExecutor struct{ dependencies ImportExecutorDependencies }
)

func NewImportExecutor(dependencies ImportExecutorDependencies) *ImportExecutor {
	return &ImportExecutor{dependencies: dependencies}
}

// Execute returns an uncommitted item outcome error before claiming any more work.
// Its caller may settle the owned execution; cancellation and lost ownership confer no write authority.
func (executor *ImportExecutor) Execute(ctx context.Context, unit Work) error {
	err := executor.execute(ctx, unit)
	if err == nil || importStopCause(ctx, err) != nil {
		return err
	}
	if failure := executor.dependencies.Settlement.Fail(
		ctx, unit.Identity(), ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
	); failure != nil {
		return fmt.Errorf("settle failed Pegasus import execution: %w", errors.Join(err, failure))
	}
	return err
}

func (executor *ImportExecutor) execute(ctx context.Context, unit Work) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop Pegasus import execution: %w", err)
		}
		cancelled, err := executor.dependencies.Settlement.Cancelled(ctx, unit.Identity())
		if err != nil {
			return fmt.Errorf("check Pegasus execution cancellation: %w", err)
		}
		if cancelled {
			return nil
		}
		item, found, err := executor.dependencies.Items.Next(ctx, unit.Identity())
		if err != nil {
			return fmt.Errorf("claim Pegasus import item: %w", err)
		}
		if !found {
			if err := executor.dependencies.Completion.Finish(ctx, unit.Identity()); err != nil {
				return fmt.Errorf("finish Pegasus import execution: %w", err)
			}
			return nil
		}
		if err := executor.Process(ctx, unit, item); err != nil {
			return err
		}
	}
}
