package sourceimport

import (
	"context"
	"fmt"

	"retrom/internal/importformat/gamelist"
)

// ScanOrganized is the only format dispatch. Everything after this projection
// consumes normalized collections, entries and files, without a format branch.
func ScanOrganized(ctx context.Context, source ScannerSource, format string, maximumYear int) (ScanResult, error) {
	if format == "" || format == "PEGASUS" {
		return NewScanner(source).Scan(ctx)
	}
	if format != "GAMELIST" {
		return ScanResult{}, ErrInvalid
	}
	reader, ok := source.(formatReader)
	if !ok {
		return ScanResult{}, fmt.Errorf("gamelist reader unavailable: %w", ErrInvalid)
	}
	result, err := gamelist.NewScanner(gamelistReader{source, reader}).Scan(ctx, maximumYear)
	if err != nil {
		return ScanResult{}, fmt.Errorf("scan gamelist: %w", err)
	}
	return normalizeGamelist(result), nil
}

type formatReader interface {
	Read(context.Context, DiscoveredFile, int64) ([]byte, error)
	Disc(context.Context, DiscoveredFile) ([]byte, error)
}
type gamelistReader struct {
	ScannerSource
	reader formatReader
}

func (source gamelistReader) Discover(ctx context.Context, visit func(gamelist.DiscoveredFile) error) error {
	err := source.ScannerSource.Discover(ctx, func(file DiscoveredFile) error {
		return visit(gamelist.DiscoveredFile(file))
	})
	if err != nil {
		return fmt.Errorf("discover gamelist files: %w", err)
	}
	return nil
}

func (source gamelistReader) Read(ctx context.Context, file gamelist.DiscoveredFile, maximum int64) ([]byte, error) {
	data, err := source.reader.Read(ctx, DiscoveredFile(file), maximum)
	if err != nil {
		return nil, fmt.Errorf("read gamelist input: %w", err)
	}
	return data, nil
}

func (source gamelistReader) Disc(ctx context.Context, file gamelist.DiscoveredFile) ([]byte, error) {
	data, err := source.reader.Disc(ctx, DiscoveredFile(file))
	if err != nil {
		return nil, fmt.Errorf("read gamelist disc: %w", err)
	}
	return data, nil
}

func (source gamelistReader) Asset(
	ctx context.Context, file gamelist.DiscoveredFile, kind string,
) (gamelist.ScanAssetInspection, error) {
	value, err := source.ScannerSource.Asset(ctx, DiscoveredFile(file), kind)
	if err != nil {
		return gamelist.ScanAssetInspection{}, fmt.Errorf("inspect gamelist asset: %w", err)
	}
	return gamelist.ScanAssetInspection{MediaType: value.MediaType, Width: value.Width, Height: value.Height}, nil
}

func normalizeGamelist(input gamelist.ScanProjection) ScanResult {
	result := ScanResult{
		SnapshotDigest: input.SnapshotDigest, EstimatedBytes: input.EstimatedBytes,
		InvalidMetadata: input.InvalidGamelists, Blocked: input.Blocked, MediaWarnings: input.MediaWarnings,
		Covers: input.Covers, Videos: input.Videos,
	}
	for _, value := range input.Gamelists {
		result.Metadata = append(result.Metadata, ScanMetadata{
			Path: value.Path, Digest: value.Digest,
			Facts: value.Facts, State: value.State, ErrorCode: value.ErrorCode, Size: value.Size,
		})
	}
	for _, value := range input.Collections {
		result.Collections = append(result.Collections, ScanCollection{
			ID: value.ID, MetadataPath: value.GamelistPath,
			Name: value.DisplayName, GameCount: value.GameCount, IssueCount: value.IssueCount,
			IgnoredJSON: "[]", WarningJSON: "[]",
		})
	}
	for _, value := range input.Items {
		result.Items = append(result.Items, normalizeGamelistItem(value))
	}
	return result
}

func normalizeGamelistItem(value gamelist.ScanItem) ScanItem {
	item := ScanItem{
		SourceFlagsJSON: value.SourceFlagsJSON, ID: value.ID, CollectionID: value.CollectionID,
		MetadataPath: value.GamelistPath,
		SourceKey:    value.SourceKey, Title: value.Title, GameOrdinal: value.GameOrdinal,
		DiscoveryState: value.DiscoveryState, DiscoveryCode: value.DiscoveryCode, MetadataJSON: value.MetadataJSON,
		WarningsJSON:       value.WarningsJSON,
		SourceManifestJSON: value.SourceManifestJSON, SourceManifestDigest: value.SourceManifestDigest,
	}
	for _, file := range value.Files {
		item.Files = append(item.Files, ScanFile(file))
	}
	for _, asset := range value.Assets {
		if asset.State != "DISCOVERED" || asset.Facts == nil || asset.Size == nil || asset.MediaType == nil {
			continue
		}
		item.Assets = append(item.Assets, ScanAsset{
			Kind: asset.Kind, Method: "EXPLICIT_GAME", Path: asset.Path,
			Facts: *asset.Facts, Size: *asset.Size, MediaType: *asset.MediaType, Width: asset.Width, Height: asset.Height,
		})
	}
	return item
}
