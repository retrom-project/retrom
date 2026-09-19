package pegasusimport

import (
	"context"
	"errors"
	"fmt"

	libraryimportmodel "retrom/internal/model/libraryimport"
	model "retrom/internal/model/pegasusimport"
)

type (
	ImportItems interface {
		Next(context.Context, model.ExecutionIdentity) (model.ExecutionItem, bool, error)
		Finish(context.Context, model.ExecutionIdentity, string, model.ItemOutcome) error
	}
	ImportMaterials interface {
		Copy(context.Context, model.ExecutionIdentity, model.MaterialSource, model.VerifiedBlob) (string, error)
		Warning(context.Context, model.ExecutionIdentity, model.MaterialSource, string) error
		SetPhase(context.Context, model.ExecutionIdentity, string) error
		Cancelled(context.Context, model.ExecutionIdentity) (bool, error)
	}
	ImportSources interface {
		CopyFile(context.Context, model.Work, model.ExecutionFile) (model.VerifiedBlob, error)
		CopyAsset(context.Context, model.Work, model.ExecutionAsset) (model.VerifiedBlob, bool, error)
	}
	ImportReviews interface {
		Resume(context.Context, model.Work, model.ExecutionItem) (bool, error)
		Create(context.Context, model.Work, model.ExecutionItem, []libraryimportmodel.ServerSourceFile) error
	}
	ImportCompanions interface {
		Find(context.Context, model.ExecutionIdentity, string) ([]model.CompanionCandidate, error)
		Record(context.Context, model.ExecutionIdentity, string, model.CompanionCandidate, model.VerifiedBlob) (string, error)
	}
	ImportSettlement interface {
		Cancelled(context.Context, model.ExecutionIdentity) (bool, error)
		Fail(context.Context, model.ExecutionIdentity, model.ExecutionFailure) error
	}
	ImportCompletion interface {
		Finish(context.Context, model.ExecutionIdentity) error
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
func (executor *ImportExecutor) Execute(ctx context.Context, unit model.Work) error {
	err := executor.execute(ctx, unit)
	if err == nil || importStopCause(ctx, err) != nil {
		return err
	}
	if failure := executor.dependencies.Settlement.Fail(
		ctx, unit.Identity(), model.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true},
	); failure != nil {
		return fmt.Errorf("settle failed Pegasus import execution: %w", errors.Join(err, failure))
	}
	return err
}

func (executor *ImportExecutor) execute(ctx context.Context, unit model.Work) error {
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
