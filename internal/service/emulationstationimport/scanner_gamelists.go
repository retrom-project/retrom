package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"

	"retrom/internal/capability/format/emulationstationmeta"
)

func (service *Scanner) projectGamelist(
	ctx context.Context,
	releaseYearMax int,
	files map[string]discoveredFile,
	caches *scanCaches,
	result *scanResult,
	gamelist *scannedGamelist,
) error {
	contents, err := service.source.Read(ctx, files[gamelist.Path], maxGamelistBytes)
	if err != nil {
		return fmt.Errorf("read EmulationStation gamelist: %w", err)
	}
	digest := sha256.Sum256(contents)
	gamelist.Digest = hex.EncodeToString(digest[:])
	document, err := emulationstationmeta.ParseContext(ctx, contents, releaseYearMax)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("emulationstationimport/parse gamelist cancelled: %w", err)
		}
		gamelist.State = "INVALID"
		gamelist.ErrorCode = parserErrorCode(err)
		result.InvalidGamelists++
		return nil
	}
	gamelist.Document = document
	collectionID, err := service.newID()
	if err != nil {
		return fmt.Errorf("generate EmulationStation collection identity: %w", err)
	}
	relativeDirectory := path.Dir(gamelist.Path)
	if relativeDirectory == "." {
		relativeDirectory = ""
	}
	displayName := path.Base(relativeDirectory)
	if relativeDirectory == "" {
		displayName = "根目录"
	}
	collection := scannedCollection{
		ID: collectionID, GamelistPath: gamelist.Path,
		RelativeDirectory: relativeDirectory, DisplayName: displayName,
		GameCount: int64(len(document.Games)), FolderEntryCount: int64(document.FolderEntryCount),
	}
	result.FolderEntries += collection.FolderEntryCount
	extensions := make(map[string]int64)
	for gameIndex := range document.Games {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("project EmulationStation game: %w", err)
		}
		if len(result.Items) >= maxGames {
			return ErrScanLimit
		}
		game := document.Games[gameIndex]
		item, err := service.projectGame(
			ctx, gamelist.Path, collection.ID, game, files, caches,
		)
		if err != nil {
			return err
		}
		collectGame(result, &collection, game, item, extensions)
	}
	collection.ExtensionSummaryJSON, collection.ExtensionOtherCount = extensionSummary(extensions)
	result.Collections = append(result.Collections, collection)
	return nil
}

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, emulationstationmeta.ErrTooLarge):
		return emulationstationmeta.ErrTooLarge.Error()
	case errors.Is(err, emulationstationmeta.ErrInvalidUTF8):
		return emulationstationmeta.ErrInvalidUTF8.Error()
	case errors.Is(err, emulationstationmeta.ErrInvalidRoot):
		return emulationstationmeta.ErrInvalidRoot.Error()
	case errors.Is(err, emulationstationmeta.ErrLimitExceeded):
		return emulationstationmeta.ErrLimitExceeded.Error()
	default:
		return emulationstationmeta.ErrInvalidXML.Error()
	}
}

func collectGame(
	result *scanResult,
	collection *scannedCollection, game emulationstationmeta.Game,
	item scannedItem, extensions map[string]int64,
) {
	if item.DiscoveryState != "READY" {
		collection.IssueCount++
		result.Blocked++
	}
	if game.SourceFlags.Hidden {
		collection.HiddenGameCount++
	}
	if game.SourceFlags.Adult {
		collection.AdultGameCount++
	}
	if game.Path != "" {
		extensions[extensionName(game.Path)]++
	}
	result.CollectItem(item)
}
