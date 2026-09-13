package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"retrom/internal/capability/format/pegasusmeta"
)

func (service *Scanner) scanGameAssets(ctx context.Context,
	metadataPath string,
	collection *pegasusmeta.Collection,
	game pegasusmeta.Game,
	files map[string]discoveredFile,
	folded map[string][]string,
) ([]scannedAsset, []map[string]any, error) {
	assets := make([]scannedAsset, 0, 2)
	warnings := make([]map[string]any, 0)
	coverDefaults, videoDefaults := []string(nil), []string(nil)
	if collection != nil {
		coverDefaults, videoDefaults = collection.Assets.Covers, collection.Assets.Videos
	}
	requests := []struct {
		kind       string
		candidates []string
		defaults   []string
	}{
		{kind: "COVER", candidates: game.Assets.Covers, defaults: coverDefaults},
		{kind: "VIDEO", candidates: game.Assets.Videos, defaults: videoDefaults},
	}
	for _, request := range requests {
		asset, assetWarnings, err := service.chooseAsset(
			ctx, metadataPath, request.kind, game.Metadata.Title,
			game.Files, request.candidates, request.defaults, files, folded,
		)
		if err != nil {
			return nil, nil, err
		}
		if asset != nil {
			assets = append(assets, *asset)
		}
		warnings = append(warnings, assetWarnings...)
	}
	return assets, warnings, nil
}

func newScannedGameItem(
	itemID, metadataPath, collectionID string,
	segmentOrdinal int,
	game pegasusmeta.Game,
	projectedFiles gameFileProjection,
	assets []scannedAsset,
	warnings []map[string]any,
) scannedItem {
	metadataJSON, _ := json.Marshal(game.Metadata)
	warningsJSON, _ := json.Marshal(warnings)
	sourceProjection := map[string]any{
		"schemaVersion":        1,
		"metadataRelativePath": metadataPath,
		"segmentOrdinal":       segmentOrdinal,
		"gameOrdinal":          game.Ordinal,
		"declaredFiles":        projectedFiles.resolvedForKey,
	}
	sourceJSON, _ := json.Marshal(sourceProjection)
	sourceDigest := sha256.Sum256(sourceJSON)
	keyJSON, _ := json.Marshal(
		map[string]any{
			"metadataRelativePath": metadataPath,
			"segmentOrdinal":       segmentOrdinal,
			"gameOrdinal":          game.Ordinal,
			"files":                projectedFiles.resolvedForKey,
		},
	)
	keyDigest := sha256.Sum256(keyJSON)
	title := game.Metadata.Title
	if title == "" || utf8.RuneCountInString(title) > pegasusmeta.MaxTitleRunes || containsControl(title) {
		title = fmt.Sprintf("Invalid game %d", game.Ordinal+1)
	}
	discoveryState := discoveryState(projectedFiles.discoveryCode)
	return scannedItem{
		ID: itemID, CollectionID: collectionID, MetadataPath: metadataPath, GameOrdinal: int64(game.Ordinal),
		SourceKey: hex.EncodeToString(keyDigest[:]), Title: title, DiscoveryState: discoveryState,
		DiscoveryCode: projectedFiles.discoveryCode,
		MetadataJSON:  string(metadataJSON), WarningsJSON: string(warningsJSON),
		SourceManifestJSON: string(
			sourceJSON,
		), SourceManifestDigest: hex.EncodeToString(sourceDigest[:]), Files: projectedFiles.files, Assets: assets,
	}
}

func discoveryState(code string) string {
	if code == "" {
		return "READY"
	}
	for _, contentCode := range []string{
		"PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED",
		"PEGASUS_COLLECTION_NAME_INVALID",
		"PEGASUS_GAME_TITLE_INVALID",
		"PEGASUS_GAME_WITHOUT_FILE",
		"PEGASUS_GAME_WITHOUT_COLLECTION",
	} {
		if code == contentCode {
			return "BLOCKED_CONTENT"
		}
	}
	return "BLOCKED_SOURCE"
}
