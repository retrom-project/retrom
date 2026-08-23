package emulationstationimport

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"retrom/internal/cleanup"
	"retrom/internal/emulationstationmeta"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
)

func (service *Service) projectAssets(
	ctx context.Context,
	root Root,
	selectedPath, gamelistPath string,
	references emulationstationmeta.AssetReferences,
	files map[string]discoveredFile,
) ([]scannedAsset, []map[string]any) {
	requests := []struct {
		kind      string
		reference *emulationstationmeta.AssetReference
	}{
		{kind: "COVER", reference: references.Cover},
		{kind: "VIDEO", reference: references.Video},
	}
	assets := make([]scannedAsset, 0, 2)
	warnings := make([]map[string]any, 0, 2)
	for _, request := range requests {
		if request.reference == nil {
			continue
		}
		asset, warning := service.projectAsset(
			ctx, root, selectedPath, gamelistPath, request.kind, *request.reference, files,
		)
		if asset != nil {
			assets = append(assets, *asset)
		}
		if warning != nil {
			warnings = append(warnings, warning)
		}
	}
	return assets, warnings
}

func (service *Service) projectAsset(
	ctx context.Context,
	root Root,
	selectedPath, gamelistPath, kind string,
	reference emulationstationmeta.AssetReference,
	files map[string]discoveredFile,
) (*scannedAsset, map[string]any) {
	if reference.RelativePath == "" {
		return nil, nil
	}
	resolved, err := resolveGamelistPath(gamelistPath, reference.RelativePath)
	if err != nil {
		return nil, scanMediaWarning("EMULATIONSTATION_PATH_INVALID", kind)
	}
	entry, exists := files[resolved]
	if !exists {
		asset := scannedAsset{
			Kind: kind, Method: reference.ResolutionMethod, Path: resolved,
			State: "MISSING", WarningCode: "EMULATIONSTATION_MEDIA_MISSING",
		}
		return &asset, scanMediaWarning(asset.WarningCode, kind)
	}
	asset := scannedAsset{
		Kind: kind, Method: reference.ResolutionMethod, Path: resolved,
		State: "DISCOVERED", Facts: stringPointer(entry.Facts), Size: int64Pointer(entry.Size),
	}
	release, acquireErr := serversource.AcquireReader(ctx)
	if acquireErr != nil {
		asset.State, asset.WarningCode = "READ_FAILED", "EMULATIONSTATION_MEDIA_READ_FAILED"
		return &asset, scanMediaWarning(asset.WarningCode, kind)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, resolved)
	if err != nil || before.Size() != entry.Size || serversource.FactsDigest(before) != entry.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		asset.State, asset.WarningCode = "SOURCE_CHANGED", "EMULATIONSTATION_SOURCE_CHANGED"
		return &asset, scanMediaWarning(asset.WarningCode, kind)
	}
	if kind == "COVER" {
		return inspectCoverAsset(handle, before, entry.Size, asset)
	}
	return inspectVideoAsset(handle, before, entry.Size, asset)
}

func inspectCoverAsset(
	handle *os.File,
	before fs.FileInfo,
	size int64,
	asset scannedAsset,
) (*scannedAsset, map[string]any) {
	image, inspectErr := mediaasset.InspectImage(handle, size)
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if inspectErr != nil || statErr != nil || !serversource.SameFileFacts(before, after) {
		asset.State, asset.WarningCode = "INVALID", "EMULATIONSTATION_IMAGE_INVALID"
		return &asset, scanMediaWarning(asset.WarningCode, asset.Kind)
	}
	asset.MediaType = stringPointer(image.MediaType)
	asset.Width, asset.Height = int64Pointer(image.WidthPX), int64Pointer(image.HeightPX)
	return &asset, nil
}

func inspectVideoAsset(
	handle *os.File,
	before fs.FileInfo,
	size int64,
	asset scannedAsset,
) (*scannedAsset, map[string]any) {
	mediaType, inspectErr := mediaasset.InspectVideo(handle, size)
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if inspectErr != nil || statErr != nil || !serversource.SameFileFacts(before, after) {
		asset.State, asset.WarningCode = "INVALID", "EMULATIONSTATION_VIDEO_UNSUPPORTED"
		if errors.Is(inspectErr, mediaasset.ErrVideoTooLarge) {
			asset.State, asset.WarningCode = "TOO_LARGE", "EMULATIONSTATION_VIDEO_TOO_LARGE"
		}
		return &asset, scanMediaWarning(asset.WarningCode, asset.Kind)
	}
	asset.MediaType = stringPointer(mediaType)
	return &asset, nil
}

func scanMediaWarning(code, kind string) map[string]any {
	return map[string]any{
		"code": code, "field": map[string]string{"COVER": "image", "VIDEO": "video"}[kind],
		"pathKind": kind,
	}
}
