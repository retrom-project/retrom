package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	library "retrom/internal/service/libraryimport"
)

func (executor *ImportExecutor) sourceFiles(
	ctx context.Context, unit Work, item ExecutionItem,
) ([]library.ServerSourceFile, error) {
	files := make([]library.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(files, library.ServerSourceFile{RelativePath: file.Path, BlobID: file.BlobID, SizeBytes: file.Size})
	}
	if item.TargetPlatformKind != "arcade" || len(item.Files) != 1 ||
		!strings.EqualFold(path.Ext(item.Files[0].Path), ".zip") {
		return files, nil
	}
	companions, err := executor.CompanionFiles(ctx, unit, item)
	if err != nil {
		return nil, err
	}
	return append(files, companions...), nil
}

func (executor *ImportExecutor) CompanionFiles(
	ctx context.Context, unit Work, item ExecutionItem,
) ([]library.ServerSourceFile, error) {
	candidates, err := executor.dependencies.Companions.Find(ctx, unit.Identity(), item.ID)
	if err != nil {
		return nil, fmt.Errorf("read Source import companions: %w", err)
	}
	files := make([]library.ServerSourceFile, 0, len(candidates))
	for _, candidate := range candidates {
		blob, err := executor.dependencies.Sources.CopyFile(ctx, unit, candidate.File)
		if stop := importStopCause(ctx, err); stop != nil {
			return nil, stop
		}
		if errors.Is(err, ErrSourceChanged) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("copy Source companion: %w", err)
		}
		blobID, err := executor.dependencies.Companions.Record(ctx, unit.Identity(), item.ID, candidate, blob)
		if err != nil {
			return nil, fmt.Errorf("bind Source companion: %w", err)
		}
		files = append(files, library.ServerSourceFile{
			RelativePath: candidate.File.Path, BlobID: blobID, SizeBytes: candidate.File.Size,
		})
	}
	return files, nil
}
