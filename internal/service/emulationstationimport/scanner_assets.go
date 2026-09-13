package emulationstationimport

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/emulationstationmeta"
	"retrom/internal/mediaasset"
)

func (service *Scanner) projectAssets(
	ctx context.Context,
	gamelistPath string,
	references emulationstationmeta.AssetReferences,
	files map[string]discoveredFile,
) ([]scannedAsset, []map[string]any, error) {
	requests := []struct {
		kind      string
		reference *emulationstationmeta.AssetReference
	}{{kind: "COVER", reference: references.Cover}, {kind: "VIDEO", reference: references.Video}}
	assets := make([]scannedAsset, 0, 2)
	warnings := make([]map[string]any, 0, 2)
	for _, request := range requests {
		if request.reference == nil {
			continue
		}
		asset, warning, err := service.projectAsset(ctx, gamelistPath, request.kind, *request.reference, files)
		if err != nil {
			return nil, nil, err
		}
		if asset != nil {
			assets = append(assets, *asset)
		}
		if warning != nil {
			warnings = append(warnings, warning)
		}
	}
	return assets, warnings, nil
}

func (service *Scanner) projectAsset(
	ctx context.Context,
	gamelistPath, kind string,
	reference emulationstationmeta.AssetReference,
	files map[string]discoveredFile,
) (*scannedAsset, map[string]any, error) {
	if reference.RelativePath == "" {
		return nil, nil, nil
	}
	resolved, valid := resolveGamelistPath(gamelistPath, reference.RelativePath)
	if !valid {
		return nil, scanMediaWarning("EMULATIONSTATION_PATH_INVALID", kind), nil
	}
	entry, exists := files[resolved]
	asset := scannedAsset{
		Kind: kind, Method: reference.ResolutionMethod, Path: resolved,
		State: "MISSING", WarningCode: "EMULATIONSTATION_MEDIA_MISSING",
	}
	if !exists {
		return &asset, scanMediaWarning(asset.WarningCode, kind), nil
	}
	asset.State, asset.WarningCode = "DISCOVERED", ""
	asset.Facts, asset.Size = &entry.Facts, &entry.Size
	inspection, err := service.source.Asset(ctx, entry, kind)
	if err != nil {
		if stop := scannerStop(ctx, err); stop != nil {
			return nil, nil, stop
		}
		asset.State, asset.WarningCode = scanAssetFailure(kind, err)
		return &asset, scanMediaWarning(asset.WarningCode, kind), nil
	}
	if inspection.Changed {
		asset.State, asset.WarningCode = scanAssetFailure(kind, nil)
		return &asset, scanMediaWarning(asset.WarningCode, kind), nil
	}
	asset.MediaType = &inspection.MediaType
	asset.Width, asset.Height = inspection.Width, inspection.Height
	return &asset, nil, nil
}

func scannerStop(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("stop EmulationStation scan inspection: %w", errors.Join(ctx.Err(), err))
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("stop EmulationStation scan inspection: %w", err)
	}
	return nil
}

func scanAssetFailure(kind string, err error) (string, string) {
	switch {
	case errors.Is(err, ErrScanReadFailed):
		return "READ_FAILED", "EMULATIONSTATION_MEDIA_READ_FAILED"
	case errors.Is(err, ErrSourceChanged):
		return "SOURCE_CHANGED", "EMULATIONSTATION_SOURCE_CHANGED"
	case kind == "COVER":
		return "INVALID", "EMULATIONSTATION_IMAGE_INVALID"
	case errors.Is(err, mediaasset.ErrVideoTooLarge):
		return "TOO_LARGE", "EMULATIONSTATION_VIDEO_TOO_LARGE"
	default:
		return "INVALID", "EMULATIONSTATION_VIDEO_UNSUPPORTED"
	}
}

func scanMediaWarning(code, kind string) map[string]any {
	return map[string]any{
		"code":     code,
		"field":    map[string]string{"COVER": "image", "VIDEO": "video"}[kind],
		"pathKind": kind,
	}
}
