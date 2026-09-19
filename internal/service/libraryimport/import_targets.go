package libraryimport

import (
	"context"

	model "retrom/internal/model/libraryimport"
)

// ReadImportTarget delegates to the model-level function.
func ReadImportTarget(ctx context.Context, reader model.ImportFactsReader, id string) (model.ImportTarget, error) {
	return model.ReadImportTarget(ctx, reader, id)
}

// ResolveImportBinding delegates to the model-level function.
func ResolveImportBinding(
	ctx context.Context, reader model.ImportFactsReader, target model.ImportTarget, generation string,
) (model.ImportTarget, error) {
	return model.ResolveImportBinding(ctx, reader, target, generation)
}

// SnapshotImportTarget delegates to the model-level function.
func SnapshotImportTarget(
	ctx context.Context, reader model.ImportFactsReader, target model.ImportTarget,
) (model.ImportTargetSnapshot, model.ImportTarget, error) {
	return model.SnapshotImportTarget(ctx, reader, target)
}

// TargetImportGuard delegates to the model-level function.
func TargetImportGuard(target model.ImportTarget) model.ImportTargetGuard {
	return model.TargetImportGuard(target)
}
