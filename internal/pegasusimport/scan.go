package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/pegasusmeta"
	"retrom/internal/serversource"
)

const (
	maxMetadataFiles = 1000
	maxGames         = 100000
)

type discoveredFile struct {
	Path, Name, Facts string
	Size              int64
}

type scannedMetadata struct {
	Path, Digest, Facts, State, ErrorCode string
	Size                                  int64
	Document                              pegasusmeta.Document
}

type scannedCollection struct {
	ID, MetadataPath, Name, Description, IgnoredJSON, WarningJSON string
	ShortName                                                     *string
	SegmentOrdinal, GameCount, IssueCount                         int64
}

type scannedItem struct {
	ID, CollectionID, MetadataPath, SourceKey, Title          string
	GameOrdinal                                               int64
	DiscoveryState, DiscoveryCode, MetadataJSON, WarningsJSON string
	SourceManifestJSON, SourceManifestDigest                  string
	Files                                                     []scannedItemFile
	Assets                                                    []scannedAsset
}

type scannedItemFile struct {
	Ordinal           int64
	Kind, Path, Facts string
	Size              int64
}

type scannedAsset struct {
	Kind, Method, Path, Facts, MediaType string
	Size                                 int64
	Width, Height                        *int64
}

type scanResult struct {
	Metadata                                []scannedMetadata
	Collections                             []scannedCollection
	Items                                   []scannedItem
	SnapshotDigest                          string
	EstimatedBytes                          int64
	InvalidMetadata, Blocked, MediaWarnings int64
	Covers, Videos                          int64
}

func (service *Service) executeScan(ctx context.Context, unit work, root Root) {
	result, err := service.scan(ctx, root, unit.RelativePath)
	if err != nil {
		service.fail(ctx, unit, errorCode(err), errors.Is(err, serversource.ErrRootUnavailable))
		return
	}
	if err := service.persistScan(ctx, unit, result); err != nil {
		service.fail(ctx, unit, "INTERNAL_ERROR", true)
	}
}

func (service *Service) scan(ctx context.Context, root Root, selectedPath string) (scanResult, error) {
	directory, err := serversource.OpenSelectedDirectory(root.path, selectedPath)
	if err != nil {
		return scanResult{}, serversource.ErrRootUnavailable
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	result := scanResult{}
	index := newScanIndex(ctx, &result)
	_, err = serversource.WalkFiles(directory, scanWalkLimits(), index.visit)
	if errors.Is(err, serversource.ErrScanLimit) {
		return scanResult{}, ErrScanLimit
	}
	if err != nil {
		return scanResult{}, fmt.Errorf("pegasusimport/walk source: %w", err)
	}
	if len(result.Metadata) == 0 {
		return scanResult{}, ErrMetadataAbsent
	}
	index.sort()
	sort.Slice(
		result.Metadata,
		func(left, right int) bool { return result.Metadata[left].Path < result.Metadata[right].Path },
	)
	for metadataIndex := range result.Metadata {
		if err := service.projectMetadata(
			root, selectedPath, &result, &result.Metadata[metadataIndex], index,
		); err != nil {
			return scanResult{}, err
		}
	}
	if result.EstimatedBytes > 2<<40 {
		return scanResult{}, ErrScanLimit
	}
	evidence := make([]map[string]any, 0, len(result.Metadata))
	for _, metadata := range result.Metadata {
		evidence = append(
			evidence,
			map[string]any{
				"path":        metadata.Path,
				"sizeBytes":   metadata.Size,
				"digest":      metadata.Digest,
				"factsDigest": metadata.Facts,
				"state":       metadata.State,
			},
		)
	}
	encoded, _ := json.Marshal(map[string]any{"schemaVersion": 1, "metadata": evidence})
	digest := sha256.Sum256(encoded)
	result.SnapshotDigest = hex.EncodeToString(digest[:])
	return result, nil
}

type scanIndex struct {
	ctx      context.Context
	result   *scanResult
	files    map[string]discoveredFile
	folded   map[string][]string
	contents map[string][]byte
}

func newScanIndex(ctx context.Context, result *scanResult) *scanIndex {
	return &scanIndex{
		ctx: ctx, result: result,
		files: map[string]discoveredFile{}, folded: map[string][]string{}, contents: map[string][]byte{},
	}
}

func scanWalkLimits() serversource.Limits {
	return serversource.Limits{MaxDepth: 64, MaxDirectories: 250000, MaxFiles: 2000000}
}

func (index *scanIndex) visit(candidate serversource.File) error {
	if err := index.ctx.Err(); err != nil {
		return fmt.Errorf("pegasusimport/scan cancelled: %w", err)
	}
	handle, info, ok := openScannedFile(candidate)
	if !ok {
		return nil
	}
	facts := serversource.FactsDigest(info)
	entry := discoveredFile{
		Path: candidate.RelativePath, Name: candidate.Basename, Size: info.Size(), Facts: facts,
	}
	index.files[entry.Path] = entry
	key := asciiFold(entry.Path)
	index.folded[key] = append(index.folded[key], entry.Path)
	if candidate.Basename != "metadata.pegasus.txt" {
		cleanup.Error("close", handle.Close())
		return nil
	}
	return index.readMetadata(handle, info, entry)
}

func openScannedFile(candidate serversource.File) (*os.File, fs.FileInfo, bool) {
	handle, info, err := serversource.OpenFile(candidate)
	return handle, info, err == nil
}

func (index *scanIndex) readMetadata(handle *os.File, info fs.FileInfo, entry discoveredFile) error {
	if len(index.result.Metadata) >= maxMetadataFiles {
		cleanup.Error("close", handle.Close())
		return ErrScanLimit
	}
	bounded, readErr := io.ReadAll(io.LimitReader(handle, pegasusmeta.MaxMetadataBytes+1))
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if readErr != nil || statErr != nil || !serversource.SameFileFacts(info, after) {
		return ErrSourceChanged
	}
	digest := sha256.Sum256(bounded)
	metadata := scannedMetadata{
		Path: entry.Path, Size: entry.Size, Facts: entry.Facts,
		Digest: hex.EncodeToString(digest[:]), State: "VALID",
	}
	if entry.Size > pegasusmeta.MaxMetadataBytes {
		metadata.State, metadata.ErrorCode = "INVALID", pegasusmeta.ErrTooLarge.Error()
		index.result.InvalidMetadata++
	} else {
		index.contents[entry.Path] = bounded
	}
	index.result.Metadata = append(index.result.Metadata, metadata)
	return nil
}

func (index *scanIndex) sort() {
	for key := range index.folded {
		sort.Strings(index.folded[key])
	}
}

func (service *Service) projectMetadata(
	root Root,
	selectedPath string,
	result *scanResult,
	metadata *scannedMetadata,
	index *scanIndex,
) error {
	if metadata.State == "INVALID" {
		return nil
	}
	document, err := pegasusmeta.Parse(index.contents[metadata.Path])
	delete(index.contents, metadata.Path)
	if err != nil {
		metadata.State, metadata.ErrorCode = "INVALID", parserErrorCode(err)
		result.InvalidMetadata++
		return nil
	}
	metadata.Document = document
	for collectionIndex := range document.Collections {
		if err := service.projectCollection(
			root, selectedPath, result, metadata.Path, &document.Collections[collectionIndex], index,
		); err != nil {
			return err
		}
	}
	for gameIndex := range document.OrphanGames {
		if len(result.Items) >= maxGames {
			return ErrScanLimit
		}
		item := service.scanGame(
			root, selectedPath, metadata.Path, "", -1, nil,
			document.OrphanGames[gameIndex], index.files, index.folded,
		)
		result.Blocked++
		result.collectItem(item)
	}
	return nil
}

func (service *Service) projectCollection(
	root Root,
	selectedPath string,
	result *scanResult,
	metadataPath string,
	collection *pegasusmeta.Collection,
	index *scanIndex,
) error {
	collectionID, _ := uuid.NewV7()
	name, invalid := projectedCollectionName(*collection)
	ignoredJSON, _ := json.Marshal(stableStrings(append([]string(nil), collection.IgnoredRules...)))
	warningJSON, _ := json.Marshal(collectionWarningFields(*collection))
	scanned := scannedCollection{
		ID: collectionID.String(), MetadataPath: metadataPath,
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
		item := service.scanGame(
			root, selectedPath, metadataPath, scanned.ID, collection.SegmentOrdinal,
			collection, game, index.files, index.folded,
		)
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

func (service *Service) scanGame(
	root Root,
	selectedPath, metadataPath, collectionID string,
	segmentOrdinal int,
	collection *pegasusmeta.Collection,
	game pegasusmeta.Game,
	files map[string]discoveredFile,
	folded map[string][]string,
) scannedItem {
	itemID, _ := uuid.NewV7()
	warnings := gameWarnings(game)
	projectedFiles := projectGameFiles(metadataPath, game.Files, files, folded, game.BlockedCode)
	assets, assetWarnings := service.scanGameAssets(
		root, selectedPath, metadataPath, collection, game, files, folded,
	)
	warnings = append(warnings, assetWarnings...)
	return newScannedGameItem(
		itemID.String(), metadataPath, collectionID, segmentOrdinal, game,
		projectedFiles, assets, warnings,
	)
}

type gameFileProjection struct {
	files          []scannedItemFile
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
	files map[string]discoveredFile,
	folded map[string][]string,
	discoveryCode string,
) gameFileProjection {
	itemFiles := make([]scannedItemFile, 0, len(declaredFiles))
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
				itemFiles,
				scannedItemFile{
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

func validateGameFileSet(current string, files []scannedItemFile, declaredCount int) string {
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

func (service *Service) scanGameAssets(
	root Root,
	selectedPath string,
	metadataPath string,
	collection *pegasusmeta.Collection,
	game pegasusmeta.Game,
	files map[string]discoveredFile,
	folded map[string][]string,
) ([]scannedAsset, []map[string]any) {
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
		asset, assetWarnings := service.chooseAsset(
			root, selectedPath, metadataPath, request.kind, game.Metadata.Title,
			game.Files, request.candidates, request.defaults, files, folded,
		)
		if asset != nil {
			assets = append(assets, *asset)
		}
		warnings = append(warnings, assetWarnings...)
	}
	return assets, warnings
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
