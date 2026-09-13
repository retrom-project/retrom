package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"retrom/internal/capability/format/pegasusmeta"

	"github.com/google/uuid"
)

const maxGames = 100000

type DiscoveredFile struct {
	Path, Name, Facts string
	Size              int64
}
type ScanAssetInspection struct {
	MediaType     string
	Width, Height *int64
}
type ScannerSource interface {
	Discover(context.Context, func(DiscoveredFile) error) error
	Metadata(context.Context, DiscoveredFile) ([]byte, error)
	Asset(context.Context, DiscoveredFile, string) (ScanAssetInspection, error)
}
type Scanner struct {
	source ScannerSource
	newID  func() (string, error)
}

func NewScanner(source ScannerSource) *Scanner { return &Scanner{source: source, newID: scannerID} }
func scannerID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create Pegasus scan identity: %w", err)
	}
	return id.String(), nil
}

type (
	discoveredFile    = DiscoveredFile
	scannedMetadata   = ScanMetadata
	scannedCollection = ScanCollection
	scannedItem       = ScanItem
	scannedItemFile   = ScanFile
	scannedAsset      = ScanAsset
	scanResult        = ScanResult
)

type ScanResult struct {
	Metadata                                                []ScanMetadata
	Collections                                             []ScanCollection
	Items                                                   []ScanItem
	SnapshotDigest                                          string
	EstimatedBytes                                          int64
	InvalidMetadata, Blocked, MediaWarnings, Covers, Videos int64
}

func (service *Scanner) Scan(ctx context.Context) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, fmt.Errorf("scan Pegasus source: %w", err)
	}
	result := ScanResult{}
	index := scanIndex{ctx: ctx, result: &result, files: map[string]DiscoveredFile{}, folded: map[string][]string{}}
	if err := service.source.Discover(ctx, index.visit); err != nil {
		return ScanResult{}, fmt.Errorf("discover Pegasus source: %w", err)
	}
	if len(result.Metadata) == 0 {
		return ScanResult{}, ErrMetadataAbsent
	}
	index.sort()
	sort.Slice(result.Metadata, func(a, b int) bool { return result.Metadata[a].Path < result.Metadata[b].Path })
	for i := range result.Metadata {
		if err := ctx.Err(); err != nil {
			return ScanResult{}, fmt.Errorf("project Pegasus metadata: %w", err)
		}
		if err := service.projectMetadata(ctx, &result, &result.Metadata[i], &index); err != nil {
			return ScanResult{}, err
		}
	}
	if result.EstimatedBytes > 2<<40 {
		return ScanResult{}, ErrScanLimit
	}
	evidence := make([]map[string]any, 0, len(result.Metadata))
	for _, metadata := range result.Metadata {
		evidence = append(evidence, map[string]any{
			"path": metadata.Path, "sizeBytes": metadata.Size, "digest": metadata.Digest,
			"factsDigest": metadata.Facts, "state": metadata.State,
		})
	}
	encoded, err := json.Marshal(map[string]any{"schemaVersion": 1, "metadata": evidence})
	if err != nil {
		return ScanResult{}, fmt.Errorf("encode Pegasus scan evidence: %w", err)
	}
	digest := sha256.Sum256(encoded)
	result.SnapshotDigest = hex.EncodeToString(digest[:])
	return result, nil
}

type scanIndex struct {
	ctx    context.Context
	result *ScanResult
	files  map[string]DiscoveredFile
	folded map[string][]string
}

func (index *scanIndex) visit(file DiscoveredFile) error {
	if err := index.ctx.Err(); err != nil {
		return fmt.Errorf("discover Pegasus entry: %w", err)
	}
	index.files[file.Path] = file
	key := asciiFold(file.Path)
	index.folded[key] = append(index.folded[key], file.Path)
	if file.Name != "metadata.pegasus.txt" {
		return nil
	}
	if len(index.result.Metadata) >= MaxMetadataFiles {
		return ErrScanLimit
	}
	metadata := ScanMetadata{Path: file.Path, Size: file.Size, Facts: file.Facts, State: "VALID"}
	if file.Size > pegasusmeta.MaxMetadataBytes {
		metadata.State, metadata.ErrorCode = "INVALID", pegasusmeta.ErrTooLarge.Error()
		index.result.InvalidMetadata++
	}
	index.result.Metadata = append(index.result.Metadata, metadata)
	return nil
}

func (index *scanIndex) sort() {
	for key := range index.folded {
		sort.Strings(index.folded[key])
	}
}
