package pegasusimport

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
)

func (service *Service) chooseAsset(
	root Root,
	selectedPath, metadataPath, kind, title string,
	declaredFiles, gameCandidates, collectionCandidates []string,
	files map[string]discoveredFile,
	folded map[string][]string,
) (*scannedAsset, []map[string]any) {
	candidates, warnings := buildAssetCandidates(
		metadataPath, kind, title, declaredFiles, gameCandidates, collectionCandidates,
	)
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		key := candidate.method + "\x00" + asciiFold(candidate.path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		asset, warning := service.resolveAssetCandidate(
			root, selectedPath, kind, candidate, files, folded,
		)
		if warning != nil {
			warnings = append(warnings, warning)
		}
		if asset != nil {
			return asset, warnings
		}
	}
	return nil, warnings
}

type assetCandidate struct {
	path, method string
	folded       bool
}

func buildAssetCandidates(
	metadataPath, kind, title string,
	declaredFiles, gameCandidates, collectionCandidates []string,
) ([]assetCandidate, []map[string]any) {
	candidates := make([]assetCandidate, 0)
	warnings := make([]map[string]any, 0)
	field := strings.ToLower(kind)
	appendExplicit := func(values []string, method string) {
		for _, value := range values {
			resolved, err := serversource.ResolveDeclaredPath(metadataPath, value)
			if err != nil {
				warnings = append(warnings, map[string]any{"code": "PEGASUS_PATH_INVALID", "field": field})
				continue
			}
			candidates = append(candidates, assetCandidate{path: resolved, method: method})
		}
	}
	appendExplicit(gameCandidates, "EXPLICIT_GAME")
	appendExplicit(collectionCandidates, "EXPLICIT_COLLECTION")
	candidates = append(candidates, automaticAssetCandidates(metadataPath, kind, title, declaredFiles)...)
	return candidates, warnings
}

func automaticAssetCandidates(
	metadataPath, kind, title string,
	declaredFiles []string,
) []assetCandidate {
	candidates := make([]assetCandidate, 0)
	base := path.Dir(metadataPath)
	if base == "." {
		base = ""
	}
	folders := []struct{ value, method string }{{title, "AUTO_TITLE"}}
	for _, declared := range declaredFiles {
		normalized, err := serversource.NormalizeDeclaredPath(declared)
		if err != nil {
			continue
		}
		name := path.Base(normalized)
		if extension := path.Ext(name); extension != "" {
			name = strings.TrimSuffix(name, extension)
		}
		folders = append(folders, struct{ value, method string }{name, "AUTO_FILE"})
	}
	basenames, extensions := []string{"boxFront", "box_front", "boxart2D"}, []string{".png", ".jpg", ".jpeg", ".webp"}
	if kind == "VIDEO" {
		basenames, extensions = []string{"video"}, []string{".mp4", ".webm"}
	}
	for _, folder := range folders {
		if folder.value == "" || containsControl(folder.value) || strings.Contains(folder.value, "/") ||
			strings.Contains(folder.value, "\\") {
			continue
		}
		for _, basename := range basenames {
			for _, extension := range extensions {
				value := path.Join(base, "media", folder.value, basename+extension)
				if serversource.ValidateRelativePath(value) == nil {
					candidates = append(
						candidates,
						assetCandidate{path: value, method: folder.method, folded: true},
					)
				}
			}
		}
	}
	return candidates
}

func (service *Service) resolveAssetCandidate(
	root Root,
	selectedPath, kind string,
	candidate assetCandidate,
	files map[string]discoveredFile,
	folded map[string][]string,
) (*scannedAsset, map[string]any) {
	resolved, warning := resolvedAssetPath(candidate, kind, folded)
	if warning != nil || resolved == "" {
		return nil, warning
	}
	entry, exists := files[resolved]
	if !exists || len(folded[asciiFold(resolved)]) > 1 {
		if candidate.folded {
			return nil, nil
		}
		return nil, assetWarning("PEGASUS_MEDIA_MISSING", kind)
	}
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, resolved)
	if err != nil || serversource.FactsDigest(before) != entry.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return nil, nil
	}
	asset := scannedAsset{
		Kind: kind, Method: candidate.method, Path: resolved, Facts: entry.Facts, Size: entry.Size,
	}
	return inspectScannedAsset(handle, before, entry, asset)
}

func resolvedAssetPath(
	candidate assetCandidate,
	kind string,
	folded map[string][]string,
) (string, map[string]any) {
	if !candidate.folded {
		return candidate.path, nil
	}
	matches := folded[asciiFold(candidate.path)]
	if len(matches) > 1 {
		return "", assetWarning("PEGASUS_MEDIA_AMBIGUOUS", kind)
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0], nil
}

func inspectScannedAsset(
	handle *os.File,
	before fs.FileInfo,
	entry discoveredFile,
	asset scannedAsset,
) (*scannedAsset, map[string]any) {
	if asset.Kind == "COVER" {
		image, inspectErr := mediaasset.InspectImage(handle, entry.Size)
		after, statErr := handle.Stat()
		cleanup.Error("close", handle.Close())
		if inspectErr != nil || statErr != nil || !serversource.SameFileFacts(before, after) {
			return nil, assetWarning("PEGASUS_IMAGE_INVALID", asset.Kind)
		}
		asset.MediaType = image.MediaType
		asset.Width, asset.Height = int64Pointer(image.WidthPX), int64Pointer(image.HeightPX)
		return &asset, nil
	}
	mediaType, inspectErr := mediaasset.InspectVideo(handle, entry.Size)
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if inspectErr == nil && statErr == nil && serversource.SameFileFacts(before, after) {
		asset.MediaType = mediaType
		return &asset, nil
	}
	code := "PEGASUS_VIDEO_UNSUPPORTED"
	if errors.Is(inspectErr, mediaasset.ErrVideoTooLarge) {
		code = "PEGASUS_VIDEO_TOO_LARGE"
	}
	return nil, assetWarning(code, asset.Kind)
}

func assetWarning(code, kind string) map[string]any {
	return map[string]any{"code": code, "field": strings.ToLower(kind)}
}
