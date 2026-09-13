package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"retrom/internal/adapter/files/mediaasset"
	"retrom/internal/adapter/files/serversource"
)

func (service *Scanner) chooseAsset(
	ctx context.Context,
	metadataPath, kind, title string,
	declaredFiles, gameCandidates, collectionCandidates []string,
	files map[string]discoveredFile,
	folded map[string][]string,
) (*scannedAsset, []map[string]any, error) {
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
		asset, warning, err := service.resolveAssetCandidate(
			ctx, kind, candidate, files, folded,
		)
		if err != nil {
			return nil, nil, err
		}
		if warning != nil {
			warnings = append(warnings, warning)
		}
		if asset != nil {
			return asset, warnings, nil
		}
	}
	return nil, warnings, nil
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

func (service *Scanner) resolveAssetCandidate(
	ctx context.Context,
	kind string,
	candidate assetCandidate,
	files map[string]discoveredFile,
	folded map[string][]string,
) (*scannedAsset, map[string]any, error) {
	resolved, warning := resolvedAssetPath(candidate, kind, folded)
	if warning != nil || resolved == "" {
		return nil, warning, nil
	}
	entry, exists := files[resolved]
	if !exists || len(folded[asciiFold(resolved)]) > 1 {
		if candidate.folded {
			return nil, nil, nil
		}
		return nil, assetWarning("PEGASUS_MEDIA_MISSING", kind), nil
	}
	inspection, err := service.source.Asset(ctx, entry, kind)
	if ctx.Err() != nil {
		return nil, nil, fmt.Errorf("inspect Pegasus asset: %w", ctx.Err())
	}
	if errors.Is(err, ErrSourceChanged) {
		return nil, nil, nil
	}
	if err != nil {
		code := "PEGASUS_IMAGE_INVALID"
		if kind == "VIDEO" {
			code = "PEGASUS_VIDEO_UNSUPPORTED"
			if errors.Is(err, mediaasset.ErrVideoTooLarge) {
				code = "PEGASUS_VIDEO_TOO_LARGE"
			}
		}
		return nil, assetWarning(code, kind), nil
	}
	return &scannedAsset{
		Kind: kind, Method: candidate.method, Path: resolved, Facts: entry.Facts, Size: entry.Size,
		MediaType: inspection.MediaType, Width: inspection.Width, Height: inspection.Height,
	}, nil, nil
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

func assetWarning(code, kind string) map[string]any {
	return map[string]any{"code": code, "field": strings.ToLower(kind)}
}
