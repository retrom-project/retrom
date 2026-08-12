package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
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
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
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

func (service *Service) persistScan(ctx context.Context, unit work, result scanResult) error {
	now := service.now().UnixMilli()
	if err := service.persistScanHeaders(ctx, unit, result, now); err != nil {
		return err
	}
	if err := service.persistScanItems(ctx, unit, result.Items, now); err != nil {
		return err
	}
	return service.finishScan(ctx, unit, result, now)
}

func (service *Service) persistScanHeaders(
	ctx context.Context,
	unit work,
	result scanResult,
	now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start scan header transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	for _, metadata := range result.Metadata {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO pegasus_import_metadata_files(
import_id,relative_path,size_bytes,content_digest,source_facts_digest,
parse_state,error_code,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)`, unit.ImportID, metadata.Path, metadata.Size, metadata.Digest,
			metadata.Facts, metadata.State, nullIfEmpty(metadata.ErrorCode), now); err != nil {
			return fmt.Errorf("pegasusimport/insert metadata evidence: %w", err)
		}
	}
	for _, collection := range result.Collections {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO pegasus_import_collections(
id,import_id,metadata_relative_path,segment_ordinal,name,shortname,description,
game_count,issue_count,ignored_rules_json,warning_fields_json,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, collection.ID, unit.ImportID, collection.MetadataPath,
			collection.SegmentOrdinal, collection.Name, collection.ShortName, collection.Description,
			collection.GameCount, collection.IssueCount, collection.IgnoredJSON,
			collection.WarningJSON, now, now); err != nil {
			return fmt.Errorf("pegasusimport/insert collection: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit scan headers: %w", err)
	}
	return nil
}

func (service *Service) persistScanItems(
	ctx context.Context,
	unit work,
	items []scannedItem,
	now int64,
) error {
	for offset := 0; offset < len(items); offset += 500 {
		end := min(offset+500, len(items))
		batch, beginErr := service.database.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("pegasusimport/start item batch: %w", beginErr)
		}
		for _, item := range items[offset:end] {
			if err := insertScannedItem(ctx, batch, unit.ImportID, item, now); err != nil {
				cleanup.Rollback(batch)
				return err
			}
		}
		if err := batch.Commit(); err != nil {
			return fmt.Errorf("pegasusimport/commit item batch: %w", err)
		}
	}
	return nil
}

func insertScannedItem(ctx context.Context, batch *sql.Tx, importID string, item scannedItem, now int64) error {
	if _, err := batch.ExecContext(ctx, `
INSERT INTO pegasus_import_items(
id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,warnings_json,source_manifest_json,
source_manifest_digest,discovery_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,'PENDING',?,?,?,?,?,?,?)`, item.ID, importID,
		nullIfEmpty(item.CollectionID), item.MetadataPath, item.GameOrdinal, item.SourceKey,
		item.Title, item.DiscoveryState, item.MetadataJSON, item.WarningsJSON,
		item.SourceManifestJSON, item.SourceManifestDigest, nullIfEmpty(item.DiscoveryCode), now, now); err != nil {
		return fmt.Errorf("pegasusimport/insert item: %w", err)
	}
	for _, file := range item.Files {
		if _, err := batch.ExecContext(ctx, `
INSERT INTO pegasus_import_item_files(
item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,
state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, file.Ordinal, file.Kind,
			file.Path, file.Size, file.Facts, now, now); err != nil {
			return fmt.Errorf("pegasusimport/insert item file: %w", err)
		}
	}
	for _, asset := range item.Assets {
		if _, err := batch.ExecContext(ctx, `
INSERT INTO pegasus_import_item_assets(
item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,
media_type,width_px,height_px,state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, asset.Kind, asset.Method,
			asset.Path, asset.Size, asset.Facts, asset.MediaType, asset.Width, asset.Height, now, now); err != nil {
			return fmt.Errorf("pegasusimport/insert asset: %w", err)
		}
	}
	return nil
}

func (service *Service) finishScan(ctx context.Context, unit work, result scanResult, now int64) error {
	finish, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start scan finish transaction: %w", err)
	}
	defer cleanup.Rollback(finish)
	processable := int64(len(result.Items)) - result.Blocked
	if _, err := finish.ExecContext(ctx, `
UPDATE pegasus_imports
SET source_snapshot_digest=?,state='AWAITING_MAPPING',phase=NULL,
metadata_count=?,invalid_metadata_count=?,collection_count=?,game_count=?,
estimated_source_bytes=?,processable_item_count=?,blocked_item_count=?,
media_warning_count=?,discovered_cover_count=?,discovered_video_count=?,
scan_completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='SCANNING'`, result.SnapshotDigest, len(result.Metadata),
		result.InvalidMetadata, len(result.Collections), len(result.Items), result.EstimatedBytes,
		processable, result.Blocked, result.MediaWarnings, result.Covers, result.Videos,
		now, now, unit.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/finish scan aggregate: %w", err)
	}
	if _, err := finish.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`, now, now, unit.JobID); err != nil {
		return fmt.Errorf("pegasusimport/finish scan job: %w", err)
	}
	data, _ := json.Marshal(
		map[string]any{
			"schemaVersion":   1,
			"metadata":        len(result.Metadata),
			"collections":     len(result.Collections),
			"games":           len(result.Items),
			"invalidMetadata": result.InvalidMetadata,
		},
	)
	if _, err := finish.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SUCCEEDED',?,?)`,
		unit.JobID, unit.ImportID, string(data), now); err != nil {
		return fmt.Errorf("pegasusimport/create scan success event: %w", err)
	}
	if err := finish.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit scan finish: %w", err)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, unit work, code string, retryable bool) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	_, _ = transaction.ExecContext(
		ctx,
		`UPDATE jobs
SET state='FAILED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=?,error_retryable=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`,
		now,
		code,
		boolInt(retryable),
		now,
		unit.JobID,
	)
	_, _ = transaction.ExecContext(
		ctx,
		`UPDATE pegasus_imports
SET state='FAILED',phase=NULL,last_error_code=?,retryable=?,completed_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=?`,
		code,
		boolInt(retryable),
		now,
		now,
		unit.ImportID,
	)
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "code": code, "retryable": retryable})
	_, _ = transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'FAILED',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	)
	_ = transaction.Commit()
}

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, pegasusmeta.ErrTooLarge):
		return pegasusmeta.ErrTooLarge.Error()
	case errors.Is(err, pegasusmeta.ErrInvalidUTF8):
		return pegasusmeta.ErrInvalidUTF8.Error()
	default:
		return pegasusmeta.ErrSyntax.Error()
	}
}

func asciiFold(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, value)
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int64Pointer(value int64) *int64 { return &value }

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ = sql.ErrNoRows
