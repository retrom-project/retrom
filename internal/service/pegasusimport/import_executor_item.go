package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	model "retrom/internal/model/pegasusimport"
	"slices"

	library "retrom/internal/model/libraryimport"
)

type importItemRun struct {
	executor *ImportExecutor
	unit     model.Work
	item     model.ExecutionItem
}

func (executor *ImportExecutor) Process(ctx context.Context, unit model.Work, item model.ExecutionItem) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop Pegasus item processing: %w", err)
	}
	item.Files, item.Assets = slices.Clone(item.Files), slices.Clone(item.Assets)
	run := importItemRun{executor: executor, unit: unit, item: item}
	resumed, err := executor.dependencies.Reviews.Resume(ctx, unit, item)
	if err != nil {
		return run.failure(ctx, "REVIEW_HANDOFF", "RESUME_REVIEW_HANDOFF", err, firstSourcePath(item))
	}
	if resumed {
		return nil
	}
	if err := executor.dependencies.Materials.SetPhase(ctx, unit.Identity(), "COPYING_CONTENT"); err != nil {
		return run.failure(ctx, "STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item))
	}
	if copied, err := run.copyFiles(ctx); !copied || err != nil {
		return err
	}
	if copied, err := run.copyAssets(ctx); !copied || err != nil {
		return err
	}
	cancelled, err := executor.dependencies.Materials.Cancelled(ctx, unit.Identity())
	if err != nil {
		return run.failure(ctx, "STORAGE", "READ_CANCELLATION", err, firstSourcePath(item))
	}
	if cancelled {
		return run.finish(ctx, nil, model.ItemOutcome{State: "CANCELLED", Code: "CANCELLED"})
	}
	return run.prepareReview(ctx)
}

func (run *importItemRun) prepareReview(ctx context.Context) error {
	files, err := run.executor.sourceFiles(ctx, run.unit, run.item)
	if err != nil {
		return run.failure(ctx, "SOURCE_ASSEMBLY", "ASSEMBLE_SOURCE_FILES", err, firstSourcePath(run.item))
	}
	if err := run.executor.dependencies.Materials.SetPhase(ctx, run.unit.Identity(), "VALIDATING"); err != nil {
		return run.failure(ctx, "STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(run.item))
	}
	if err := run.executor.dependencies.Reviews.Create(ctx, run.unit, run.item, files); err != nil {
		return run.finish(ctx, err, model.ItemOutcome{
			State: "COMMIT_FAILED", Code: "PEGASUS_LIBRARY_IMPORT_FAILED", Retryable: true,
			Failure: LibraryFailureDetails(run.executor.dependencies.Diagnostics, err, files),
		})
	}
	return nil
}

func (run *importItemRun) failure(ctx context.Context, stage, operation string, err error, path string) error {
	return run.finish(ctx, err, model.ItemOutcome{
		State: "COMMIT_FAILED", Code: "INTERNAL_ERROR", Retryable: true,
		Failure: DescribeFailure(run.executor.dependencies.Diagnostics, stage, operation, err, path),
	})
}

func (run *importItemRun) finish(ctx context.Context, cause error, outcome model.ItemOutcome) error {
	if stop := importStopCause(ctx, cause); stop != nil {
		return fmt.Errorf("stop Pegasus item: %w", stop)
	}
	if err := run.executor.dependencies.Items.Finish(ctx, run.unit.Identity(), run.item.ID, outcome); err != nil {
		return fmt.Errorf("persist Pegasus item outcome: %w", errors.Join(cause, err))
	}
	return nil
}

func importStopCause(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	if errors.Is(err, model.ErrVersionConflict) || errors.Is(err, library.ErrVersionConflict) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func firstSourcePath(item model.ExecutionItem) string {
	if len(item.Files) == 0 {
		return ""
	}
	return item.Files[0].Path
}
