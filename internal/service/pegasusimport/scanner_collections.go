package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"retrom/internal/pegasusmeta"
)

func (service *Scanner) projectMetadata(ctx context.Context,
	result *scanResult,
	metadata *scannedMetadata,
	index *scanIndex,
) error {
	if metadata.State == "INVALID" {
		return nil
	}
	contents, err := service.source.Metadata(ctx, index.files[metadata.Path])
	if err != nil {
		return fmt.Errorf("read Pegasus metadata: %w", err)
	}
	digest := sha256.Sum256(contents)
	metadata.Digest = hex.EncodeToString(digest[:])
	document, err := pegasusmeta.Parse(contents)
	if err != nil {
		metadata.State, metadata.ErrorCode = "INVALID", parserErrorCode(err)
		result.InvalidMetadata++
		return nil
	}
	for collectionIndex := range document.Collections {
		if err := service.projectCollection(
			ctx, result, metadata.Path, &document.Collections[collectionIndex], index,
		); err != nil {
			return err
		}
	}
	for gameIndex := range document.OrphanGames {
		if len(result.Items) >= maxGames {
			return ErrScanLimit
		}
		item, err := service.scanGame(
			ctx, metadata.Path, "", -1, nil,
			document.OrphanGames[gameIndex], index.files, index.folded,
		)
		if err != nil {
			return err
		}
		result.Blocked++
		result.collectItem(item)
	}
	return nil
}

func (service *Scanner) projectCollection(
	ctx context.Context,
	result *scanResult,
	metadataPath string,
	collection *pegasusmeta.Collection,
	index *scanIndex,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("project Pegasus collection: %w", err)
	}
	collectionID, err := service.newID()
	if err != nil {
		return fmt.Errorf("create Pegasus collection identity: %w", err)
	}
	name, invalid := projectedCollectionName(*collection)
	ignoredJSON, _ := json.Marshal(stableStrings(append([]string(nil), collection.IgnoredRules...)))
	warningJSON, _ := json.Marshal(collectionWarningFields(*collection))
	scanned := scannedCollection{
		ID: collectionID, MetadataPath: metadataPath,
		SegmentOrdinal: int64(collection.SegmentOrdinal), Name: name,
		ShortName: stringPointer(collection.ShortName), Description: collection.Description,
		GameCount: int64(len(collection.Games)), IgnoredJSON: string(ignoredJSON), WarningJSON: string(warningJSON),
	}
	for gameIndex := range collection.Games {
		if len(result.Items) >= maxGames {
			return ErrScanLimit
		}
		game := collection.Games[gameIndex]
		if invalid && game.BlockedCode == "" {
			game.BlockedCode = "PEGASUS_COLLECTION_NAME_INVALID"
		}
		item, err := service.scanGame(
			ctx, metadataPath, scanned.ID, collection.SegmentOrdinal,
			collection, game, index.files, index.folded,
		)
		if err != nil {
			return err
		}
		if item.DiscoveryState != "READY" {
			scanned.IssueCount++
			result.Blocked++
		}
		result.collectItem(item)
	}
	result.Collections = append(result.Collections, scanned)
	return nil
}

func projectedCollectionName(collection pegasusmeta.Collection) (string, bool) {
	for _, warning := range collection.Warnings {
		if warning.Code == "PEGASUS_COLLECTION_NAME_INVALID" {
			return fmt.Sprintf("Invalid collection %d", collection.SegmentOrdinal+1), true
		}
	}
	return collection.Name, false
}

func collectionWarningFields(collection pegasusmeta.Collection) []string {
	fields := append([]string(nil), collection.UnknownFields...)
	for _, warning := range collection.Warnings {
		fields = append(fields, warning.Field)
	}
	return stableStrings(fields)
}

func (result *scanResult) collectItem(item scannedItem) {
	result.Items = append(result.Items, item)
	for _, file := range item.Files {
		result.addEstimated(file.Size)
	}
	for _, asset := range item.Assets {
		result.addEstimated(asset.Size)
		if asset.Kind == "COVER" {
			result.Covers++
		} else {
			result.Videos++
		}
	}
	var warnings []map[string]any
	_ = json.Unmarshal([]byte(item.WarningsJSON), &warnings)
	for _, warning := range warnings {
		if code, _ := warning["code"].(string); strings.HasPrefix(code, "PEGASUS_IMAGE_") ||
			strings.HasPrefix(code, "PEGASUS_VIDEO_") ||
			code == "PEGASUS_MEDIA_AMBIGUOUS" ||
			code == "PEGASUS_MEDIA_MISSING" {
			result.MediaWarnings++
		}
	}
}

func (result *scanResult) addEstimated(value int64) {
	const maximum = int64(2 << 40)
	if value < 0 || result.EstimatedBytes > maximum-value {
		result.EstimatedBytes = maximum + 1
		return
	}
	result.EstimatedBytes += value
}
