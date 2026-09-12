package pegasusimport

import (
	"context"
	"errors"
)

func (run *importItemRun) copyFiles(ctx context.Context) (bool, error) {
	for index, file := range run.item.Files {
		blob, err := run.executor.dependencies.Sources.CopyFile(ctx, run.unit, file)
		if err != nil {
			outcome := ItemOutcome{State: "READ_FAILED", Code: "READ_FAILED", Retryable: true}
			if errors.Is(err, ErrSourceChanged) {
				outcome = ItemOutcome{State: "SOURCE_CHANGED", Code: "PEGASUS_SOURCE_CHANGED"}
			}
			return false, run.finish(ctx, err, outcome)
		}
		blobID, err := run.executor.dependencies.Materials.Copy(ctx, run.unit.Identity(), MaterialSource{
			Key:  MaterialKey{ItemID: run.item.ID, Ordinal: file.Ordinal},
			Path: file.Path, Facts: file.Facts, Size: file.Size,
		}, blob)
		if err != nil {
			return false, run.finish(ctx, err, ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR", Retryable: true})
		}
		run.item.Files[index].BlobID = blobID
	}
	return true, nil
}

func (run *importItemRun) copyAssets(ctx context.Context) (bool, error) {
	for index, asset := range run.item.Assets {
		blob, valid, err := run.executor.dependencies.Sources.CopyAsset(ctx, run.unit, asset)
		if stop := importStopCause(ctx, err); stop != nil {
			return false, stop
		}
		if err != nil && !errors.Is(err, ErrSourceChanged) {
			return false, run.failure(ctx, "STORAGE", "COPY_MEDIA", err, asset.Path)
		}
		source := AssetMaterial(run.item.ID, asset)
		if err != nil || !valid {
			if err := run.executor.dependencies.Materials.Warning(
				ctx, run.unit.Identity(), source, mediaWarning(asset.Kind, err),
			); err != nil {
				return false, run.failure(ctx, "STORAGE", "WRITE_MEDIA_WARNING", err, asset.Path)
			}
			continue
		}
		blobID, err := run.executor.dependencies.Materials.Copy(ctx, run.unit.Identity(), source, blob)
		if err != nil {
			return false, run.failure(ctx, "STORAGE", "BIND_MEDIA", err, asset.Path)
		}
		run.item.Assets[index].BlobID = blobID
	}
	return true, nil
}

func AssetMaterial(itemID string, asset ExecutionAsset) MaterialSource {
	return MaterialSource{
		Key: MaterialKey{ItemID: itemID, Kind: asset.Kind}, Path: asset.Path, Facts: asset.Facts,
		Size: asset.Size, MediaType: asset.MediaType, Width: asset.Width, Height: asset.Height,
	}
}

func mediaWarning(kind string, err error) string {
	if errors.Is(err, ErrSourceChanged) {
		return "PEGASUS_SOURCE_CHANGED"
	}
	if kind == "COVER" {
		return "PEGASUS_IMAGE_INVALID"
	}
	return "PEGASUS_VIDEO_UNSUPPORTED"
}
