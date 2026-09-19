package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	libraryimportmodel "retrom/internal/model/libraryimport"
	model "retrom/internal/model/pegasusimport"
)

func (executor *ImportExecutor) sourceFiles(
	ctx context.Context, unit model.Work, item model.ExecutionItem,
) ([]libraryimportmodel.ServerSourceFile, error) {
	files := make([]libraryimportmodel.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(files, libraryimportmodel.ServerSourceFile{
			RelativePath: file.Path,
			BlobID:       file.BlobID,
			SizeBytes:    file.Size,
		})
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
	ctx context.Context, unit model.Work, item model.ExecutionItem,
) ([]libraryimportmodel.ServerSourceFile, error) {
	candidates, err := executor.dependencies.Companions.Find(ctx, unit.Identity(), item.ID)
	if err != nil {
		return nil, fmt.Errorf("read Pegasus import companions: %w", err)
	}
	files := make([]libraryimportmodel.ServerSourceFile, 0, len(candidates))
	for _, candidate := range candidates {
		blob, err := executor.dependencies.Sources.CopyFile(ctx, unit, candidate.File)
		if stop := importStopCause(ctx, err); stop != nil {
			return nil, stop
		}
		if errors.Is(err, model.ErrSourceChanged) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("copy Pegasus companion: %w", err)
		}
		blobID, err := executor.dependencies.Companions.Record(ctx, unit.Identity(), item.ID, candidate, blob)
		if err != nil {
			return nil, fmt.Errorf("bind Pegasus companion: %w", err)
		}
		files = append(files, libraryimportmodel.ServerSourceFile{
			RelativePath: candidate.File.Path, BlobID: blobID, SizeBytes: candidate.File.Size,
		})
	}
	return files, nil
}
