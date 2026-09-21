package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"retrom/internal/emulationstationmeta"
	"retrom/internal/multidisc"

	"github.com/google/uuid"
)

const (
	maxGamelists      = 1_000
	maxGamelistBytes  = int64(emulationstationmeta.MaxGameListBytes)
	maxGamelistsBytes = 64 << 20
	maxGames          = 100_000
)

var (
	ErrGamelistAbsent  = errors.New("EMULATIONSTATION_GAMELIST_NOT_FOUND")
	ErrNoValidGamelist = errors.New("EMULATIONSTATION_NO_VALID_GAMELIST")
	ErrScanLimit       = errors.New("EMULATIONSTATION_SCAN_LIMIT_EXCEEDED")
	ErrScanReadFailed  = errors.New("EmulationStation source inspection failed")
)

type DiscoveredFile struct {
	Path, Name, Facts string
	Size              int64
}
type ScanAssetInspection struct {
	Changed       bool
	MediaType     string
	Width, Height *int64
}
type ScannerSource interface {
	Discover(context.Context, func(DiscoveredFile) error) error
	Read(context.Context, DiscoveredFile, int64) ([]byte, error)
	Disc(context.Context, DiscoveredFile) ([]byte, error)
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
		return "", fmt.Errorf("create EmulationStation scan identity: %w", err)
	}
	return id.String(), nil
}

type (
	discoveredFile    = DiscoveredFile
	scannedGamelist   = ScanGamelist
	scannedCollection = ScanCollection
	scannedItem       = ScanItem
	scannedItemFile   = ScanItemFile
	scannedAsset      = ScanAsset
	scanResult        = ScanProjection
)

func (service *Scanner) Scan(
	ctx context.Context,
	releaseYearMax int,
) (scanResult, error) {
	if err := ctx.Err(); err != nil {
		return scanResult{}, fmt.Errorf("scan EmulationStation source: %w", err)
	}
	index := &scanIndex{ctx: ctx, files: make(map[string]discoveredFile)}
	if err := service.source.Discover(ctx, index.visit); err != nil {
		return scanResult{}, fmt.Errorf("discover EmulationStation source: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return scanResult{}, fmt.Errorf("complete EmulationStation discovery: %w", err)
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
		if err := ctx.Err(); err != nil {
			return scanResult{}, fmt.Errorf("project EmulationStation gamelist: %w", err)
		}
		if result.Gamelists[gamelistIndex].State == "INVALID" {
			result.InvalidGamelists++
			continue
		}
		if err := service.projectGamelist(
			ctx, releaseYearMax, index.files, &caches,
			&result, &result.Gamelists[gamelistIndex],
		); err != nil {
			return scanResult{}, err
		}
		if result.Gamelists[gamelistIndex].State == "VALID" {
			valid++
		}
	}
	if err := ctx.Err(); err != nil {
		return scanResult{}, fmt.Errorf("complete EmulationStation projection: %w", err)
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

func (index *scanIndex) visit(candidate DiscoveredFile) error {
	if err := index.ctx.Err(); err != nil {
		return fmt.Errorf("emulationstationimport/scan cancelled: %w", err)
	}
	entry := candidate
	index.files[entry.Path] = entry
	if candidate.Name != "gamelist.xml" {
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
