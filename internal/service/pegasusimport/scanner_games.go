package pegasusimport

import (
	"context"
	"fmt"
	"path"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/capability/format/pegasusmeta"
	model "retrom/internal/model/pegasusimport"
)

func (service *Scanner) scanGame(ctx context.Context, metadataPath, collectionID string, segmentOrdinal int,
	collection *pegasusmeta.Collection,
	game pegasusmeta.Game,
	files map[string]DiscoveredFile,
	folded map[string][]string,
) (model.ScanItem, error) {
	if err := ctx.Err(); err != nil {
		return model.ScanItem{}, fmt.Errorf("project Pegasus game: %w", err)
	}
	itemID, err := service.newID()
	if err != nil {
		return model.ScanItem{}, fmt.Errorf("create Pegasus game identity: %w", err)
	}
	warnings := gameWarnings(game)
	projectedFiles := projectGameFiles(metadataPath, game.Files, files, folded, game.BlockedCode)
	assets, assetWarnings, err := service.scanGameAssets(
		ctx, metadataPath, collection, game, files, folded,
	)
	if err != nil {
		return model.ScanItem{}, err
	}
	warnings = append(warnings, assetWarnings...)
	return newScannedGameItem(
		itemID, metadataPath, collectionID, segmentOrdinal, game,
		projectedFiles, assets, warnings,
	), nil
}

type gameFileProjection struct {
	files          []model.ScanFile
	resolvedForKey []string
	discoveryCode  string
}

func gameWarnings(game pegasusmeta.Game) []map[string]any {
	warnings := make([]map[string]any, 0, len(game.Warnings)+len(game.UnknownFields))
	for _, warning := range game.Warnings {
		warnings = append(warnings, map[string]any{"code": warning.Code, "field": warning.Field})
	}
	for _, field := range game.UnknownFields {
		warnings = append(warnings, map[string]any{"code": "FIELD_IGNORED", "field": field})
	}
	return warnings
}

func projectGameFiles(
	metadataPath string,
	declaredFiles []string,
	files map[string]DiscoveredFile,
	folded map[string][]string,
	discoveryCode string,
) gameFileProjection {
	itemFiles := make([]model.ScanFile, 0, len(declaredFiles))
	seenPaths := map[string]struct{}{}
	resolvedForKey := make([]string, 0, len(declaredFiles))
	for ordinal, declared := range declaredFiles {
		resolved, err := serversource.ResolveDeclaredPath(metadataPath, declared)
		if err != nil {
			discoveryCode = firstDiscoveryCode(discoveryCode, "PEGASUS_PATH_INVALID")
			resolvedForKey = append(resolvedForKey, declared)
			continue
		}
		resolvedForKey = append(resolvedForKey, resolved)
		fold := asciiFold(resolved)
		if _, exists := seenPaths[fold]; exists || len(folded[fold]) > 1 {
			discoveryCode = firstDiscoveryCode(discoveryCode, "PEGASUS_PATH_INVALID")
			continue
		}
		seenPaths[fold] = struct{}{}
		candidate, exists := files[resolved]
		if !exists {
			discoveryCode = firstDiscoveryCode(discoveryCode, "PEGASUS_SOURCE_NOT_REGULAR")
			continue
		}
		kind := gameFileKind(resolved)
		// The persisted projection is deliberately bounded even for an already
		// blocked entry, while resolvedForKey retains every declared reference so
		// that the deterministic source identity cannot collapse two bad entries.
		if ordinal < pegasusmeta.MaxGameFileValues {
			itemFiles = append(
				itemFiles, model.ScanFile{
					Ordinal: int64(ordinal),
					Kind:    kind,
					Path:    resolved,
					Size:    candidate.Size,
					Facts:   candidate.Facts,
				},
			)
		}
	}
	discoveryCode = validateGameFileSet(discoveryCode, itemFiles, len(declaredFiles))
	return gameFileProjection{
		files: itemFiles, resolvedForKey: resolvedForKey, discoveryCode: discoveryCode,
	}
}

func firstDiscoveryCode(current, fallback string) string {
	if current != "" {
		return current
	}
	return fallback
}

func gameFileKind(relativePath string) string {
	switch asciiFold(path.Ext(relativePath)) {
	case ".m3u":
		return "PLAYLIST"
	case ".chd":
		return "DISC"
	default:
		return "FILE"
	}
}

func validateGameFileSet(current string, files []model.ScanFile, declaredCount int) string {
	if declaredCount == 0 {
		return firstDiscoveryCode(current, "PEGASUS_GAME_WITHOUT_FILE")
	}
	if declaredCount == 1 {
		return current
	}
	playlistCount, discCount := 0, 0
	for _, file := range files {
		switch file.Kind {
		case "PLAYLIST":
			playlistCount++
		case "DISC":
			discCount++
		}
	}
	if playlistCount != 1 || discCount < 2 || discCount > 8 || len(files) != declaredCount {
		return firstDiscoveryCode(current, "PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED")
	}
	return current
}
