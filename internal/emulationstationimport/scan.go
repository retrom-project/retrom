package emulationstationimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/emulationstationmeta"
	"retrom/internal/multidisc"
	"retrom/internal/serversource"
)

const (
	maxGamelists      = 1_000
	maxGamelistBytes  = int64(emulationstationmeta.MaxGameListBytes)
	maxGamelistsBytes = 64 << 20
	maxGames          = 100_000
)

type discoveredFile struct {
	Path, Name, Facts string
	Size              int64
}

type scannedGamelist struct {
	Path, Digest, Facts, State, ErrorCode string
	Size                                  int64
	Document                              emulationstationmeta.Document
}

type scannedCollection struct {
	ID, GamelistPath, RelativeDirectory, DisplayName string
	GameCount, IssueCount, FolderEntryCount          int64
	HiddenGameCount, AdultGameCount                  int64
	ExtensionSummaryJSON                             string
	ExtensionOtherCount                              int64
}

type scannedItem struct {
	ID, CollectionID, GamelistPath, SourceKey, Title string
	GameOrdinal                                      int64
	SourceFlagsJSON                                  string
	DiscoveryState, DiscoveryCode                    string
	ContentKind, MetadataJSON, WarningsJSON          string
	SourceManifestJSON, SourceManifestDigest         string
	Files                                            []scannedItemFile
	Assets                                           []scannedAsset
}

type scannedItemFile struct {
	Ordinal           int64
	Kind, Path, Facts string
	Size              int64
}

type scannedAsset struct {
	Kind, Method, Path, State, WarningCode string
	Facts, MediaType                       *string
	Size, Width, Height                    *int64
}

type scanResult struct {
	Gamelists                              []scannedGamelist
	Collections                            []scannedCollection
	Items                                  []scannedItem
	SnapshotDigest                         string
	EstimatedBytes                         int64
	InvalidGamelists, FolderEntries        int64
	Blocked, MediaWarnings, Covers, Videos int64
}

func (service *Service) executeScan(ctx context.Context, unit work, root Root) {
	if err := service.clearScanStaging(ctx, unit.ImportID); err != nil {
		service.fail(ctx, unit, "INTERNAL_ERROR", true)
		return
	}
	result, err := service.scan(ctx, root, unit.RelativePath, unit.ReleaseYearMax)
	if err != nil {
		if errors.Is(err, ErrNoValidGamelist) {
			if evidenceErr := service.persistRejectedScan(ctx, unit, result); evidenceErr != nil {
				service.fail(ctx, unit, "INTERNAL_ERROR", true)
				return
			}
		}
		service.fail(ctx, unit, errorCode(err), errors.Is(err, serversource.ErrRootUnavailable))
		return
	}
	if err := service.persistScan(ctx, unit, result); err != nil {
		_ = service.clearScanStaging(ctx, unit.ImportID)
		service.fail(ctx, unit, "INTERNAL_ERROR", true)
	}
}

func (service *Service) scan(
	ctx context.Context,
	root Root,
	selectedPath string,
	releaseYearMax int,
) (scanResult, error) {
	directory, err := serversource.OpenSelectedDirectory(root.path, selectedPath)
	if err != nil {
		return scanResult{}, serversource.ErrRootUnavailable
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	index := &scanIndex{ctx: ctx, files: make(map[string]discoveredFile)}
	_, err = serversource.WalkFiles(directory, scanWalkLimits(), index.visit)
	if errors.Is(err, serversource.ErrScanLimit) || errors.Is(err, ErrScanLimit) {
		return scanResult{}, ErrScanLimit
	}
	if err != nil {
		return scanResult{}, fmt.Errorf("emulationstationimport/walk source: %w", err)
	}
	if len(index.gamelists) == 0 {
		return scanResult{}, ErrGamelistAbsent
	}
	sort.Slice(index.gamelists, func(left, right int) bool {
		return index.gamelists[left].Path < index.gamelists[right].Path
	})
	result := scanResult{Gamelists: index.gamelists}
	caches := scanCaches{discCandidates: make(map[string][]multidisc.File)}
	valid := 0
	for gamelistIndex := range result.Gamelists {
		if result.Gamelists[gamelistIndex].State == "INVALID" {
			result.InvalidGamelists++
			continue
		}
		if err := service.projectGamelist(
			ctx, root, selectedPath, releaseYearMax, index.files, &caches,
			&result, &result.Gamelists[gamelistIndex],
		); err != nil {
			return scanResult{}, err
		}
		if result.Gamelists[gamelistIndex].State == "VALID" {
			valid++
		}
	}
	result.SnapshotDigest = snapshotDigest(result.Gamelists)
	if valid == 0 {
		return result, ErrNoValidGamelist
	}
	if result.EstimatedBytes > 2<<40 {
		return scanResult{}, ErrScanLimit
	}
	return result, nil
}

type scanIndex struct {
	ctx           context.Context
	files         map[string]discoveredFile
	gamelists     []scannedGamelist
	gamelistBytes int64
}

func scanWalkLimits() serversource.Limits {
	return serversource.Limits{MaxDepth: 64, MaxDirectories: 250_000, MaxFiles: 2_000_000}
}

func (index *scanIndex) visit(candidate serversource.File) error {
	if err := index.ctx.Err(); err != nil {
		return fmt.Errorf("emulationstationimport/scan cancelled: %w", err)
	}
	release, err := serversource.AcquireReader(index.ctx)
	if err != nil {
		return fmt.Errorf("emulationstationimport/acquire discovery reader: %w", err)
	}
	handle, info, err := serversource.OpenFile(candidate)
	if err != nil {
		release()
		return fmt.Errorf("emulationstationimport/open discovered source: %w", serversource.ErrRootUnavailable)
	}
	entry := discoveredFile{
		Path: candidate.RelativePath, Name: candidate.Basename,
		Size: info.Size(), Facts: serversource.FactsDigest(info),
	}
	index.files[entry.Path] = entry
	cleanup.Error("close", handle.Close())
	release()
	if candidate.Basename != "gamelist.xml" {
		return nil
	}
	if len(index.gamelists) >= maxGamelists || entry.Size < 0 {
		return ErrScanLimit
	}
	if entry.Size > maxGamelistBytes {
		index.gamelists = append(index.gamelists, scannedGamelist{
			Path: entry.Path, Size: entry.Size, Facts: entry.Facts, State: "INVALID",
			ErrorCode: emulationstationmeta.ErrTooLarge.Error(),
		})
		return nil
	}
	if index.gamelistBytes > maxGamelistsBytes-entry.Size {
		return ErrScanLimit
	}
	index.gamelistBytes += entry.Size
	index.gamelists = append(index.gamelists, scannedGamelist{
		Path: entry.Path, Size: entry.Size, Facts: entry.Facts, State: "VALID",
	})
	return nil
}

func (service *Service) projectGamelist(
	ctx context.Context,
	root Root,
	selectedPath string,
	releaseYearMax int,
	files map[string]discoveredFile,
	caches *scanCaches,
	result *scanResult,
	gamelist *scannedGamelist,
) error {
	contents, digest, err := readFrozenFile(
		ctx, root, selectedPath, files[gamelist.Path], maxGamelistBytes,
	)
	if err != nil {
		return err
	}
	gamelist.Digest = digest
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
	collectionID, _ := uuid.NewV7()
	relativeDirectory := path.Dir(gamelist.Path)
	if relativeDirectory == "." {
		relativeDirectory = ""
	}
	displayName := path.Base(relativeDirectory)
	if relativeDirectory == "" {
		displayName = "根目录"
	}
	collection := scannedCollection{
		ID: collectionID.String(), GamelistPath: gamelist.Path,
		RelativeDirectory: relativeDirectory, DisplayName: displayName,
		GameCount: int64(len(document.Games)), FolderEntryCount: int64(document.FolderEntryCount),
	}
	result.FolderEntries += collection.FolderEntryCount
	extensions := make(map[string]int64)
	for gameIndex := range document.Games {
		if len(result.Items) >= maxGames {
			return ErrScanLimit
		}
		game := document.Games[gameIndex]
		item := service.projectGame(
			ctx, root, selectedPath, gamelist.Path, collection.ID, game, files, caches,
		)
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
		result.collectItem(item)
	}
	collection.ExtensionSummaryJSON, collection.ExtensionOtherCount = extensionSummary(extensions)
	result.Collections = append(result.Collections, collection)
	return nil
}

func readFrozenFile(
	ctx context.Context,
	root Root,
	selectedPath string,
	entry discoveredFile,
	maximum int64,
) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", fmt.Errorf("emulationstationimport/scan cancelled: %w", err)
	}
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("emulationstationimport/acquire scan reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, entry.Path)
	if err != nil {
		return nil, "", classifyFrozenOpenError(err)
	}
	if before.Size() != entry.Size || serversource.FactsDigest(before) != entry.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return nil, "", ErrSourceChanged
	}
	contents, readErr := io.ReadAll(io.LimitReader(handle, maximum+1))
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if readErr != nil || statErr != nil {
		return nil, "", fmt.Errorf("emulationstationimport/read frozen source: %w", serversource.ErrRootUnavailable)
	}
	if int64(len(contents)) != entry.Size ||
		!serversource.SameFileFacts(before, after) || len(contents) > int(maximum) {
		return nil, "", ErrSourceChanged
	}
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:]), nil
}

func classifyFrozenOpenError(err error) error {
	if errors.Is(err, serversource.ErrRootUnavailable) {
		return serversource.ErrRootUnavailable
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, serversource.ErrPathInvalid) ||
		errors.Is(err, serversource.ErrSourceChanged) {
		return ErrSourceChanged
	}
	return fmt.Errorf("emulationstationimport/open frozen source: %w", serversource.ErrRootUnavailable)
}

func snapshotDigest(gamelists []scannedGamelist) string {
	type gamelistSnapshot struct {
		Path          string  `json:"path"`
		SizeBytes     int64   `json:"sizeBytes"`
		ContentDigest *string `json:"contentDigest"`
		FactsDigest   string  `json:"factsDigest"`
		ParseState    string  `json:"parseState"`
	}
	type sourceSnapshot struct {
		SchemaVersion int                `json:"schemaVersion"`
		Gamelists     []gamelistSnapshot `json:"gamelists"`
	}
	values := make([]gamelistSnapshot, 0, len(gamelists))
	for _, gamelist := range gamelists {
		var contentDigest *string
		if gamelist.Digest != "" {
			contentDigest = &gamelist.Digest
		}
		values = append(values, gamelistSnapshot{
			Path: gamelist.Path, SizeBytes: gamelist.Size,
			ContentDigest: contentDigest, FactsDigest: gamelist.Facts,
			ParseState: gamelist.State,
		})
	}
	encoded := compactJSON(sourceSnapshot{SchemaVersion: 1, Gamelists: values})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func compactJSON(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
}

func extensionName(value string) string {
	extension := strings.ToLower(path.Ext(path.Base(value)))
	if extension == "" {
		return "(none)"
	}
	return extension
}

func extensionSummary(values map[string]int64) (string, int64) {
	type pair struct {
		Extension string `json:"extension"`
		Count     int64  `json:"count"`
	}
	items := make([]pair, 0, len(values))
	for extension, count := range values {
		items = append(items, pair{Extension: extension, Count: count})
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}
		return items[left].Extension < items[right].Extension
	})
	var other int64
	if len(items) > 32 {
		for _, item := range items[32:] {
			other += item.Count
		}
		items = items[:32]
	}
	return string(compactJSON(items)), other
}
