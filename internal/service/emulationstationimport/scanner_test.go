package emulationstationimport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"retrom/internal/emulationstationmeta"
)

type scannerMemory struct {
	files             []DiscoveredFile
	data              map[string][]byte
	readErr, assetErr error
	reads             []string
}

func (memory *scannerMemory) Discover(ctx context.Context, visit func(DiscoveredFile) error) error {
	for _, file := range memory.files {
		if err := visit(file); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (memory *scannerMemory) Read(_ context.Context, file DiscoveredFile, _ int64) ([]byte, error) {
	memory.reads = append(memory.reads, file.Path)
	return memory.data[file.Path], memory.readErr
}

func (memory *scannerMemory) Disc(context.Context, DiscoveredFile) ([]byte, error) {
	return []byte("MComprHD"), memory.readErr
}

func (memory *scannerMemory) Asset(context.Context, DiscoveredFile, string) (ScanAssetInspection, error) {
	return ScanAssetInspection{MediaType: "image/png"}, memory.assetErr
}

func newScannerMemory() *scannerMemory {
	xml := []byte(
		`<gameList><game><path>game.nes</path><name>Example</name><releasedate>20290101T000000</releasedate></game></gameList>`,
	)
	return &scannerMemory{
		files: []DiscoveredFile{{Path: "gamelist.xml", Name: "gamelist.xml", Facts: "xml", Size: int64(len(xml))}, {Path: "game.nes", Name: "game.nes", Facts: "rom", Size: 3}},
		data:  map[string][]byte{"gamelist.xml": xml},
	}
}

func TestScannerProjectsFrozenYearAndSourceFacts(t *testing.T) {
	memory := newScannerMemory()
	scanner := NewScanner(memory)
	result, err := scanner.Scan(t.Context(), 2027)
	if err != nil || len(
		result.Items,
	) != 1 || result.Items[0].Files[0].Facts != "rom" || result.Collections[0].DisplayName != "根目录" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	var metadata emulationstationmeta.Metadata
	if err := json.Unmarshal([]byte(result.Items[0].MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ReleaseYear != nil {
		t.Fatalf("frozen year maximum ignored: %v", *metadata.ReleaseYear)
	}
	future, err := scanner.Scan(t.Context(), 2030)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(future.Items[0].MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ReleaseYear == nil || *metadata.ReleaseYear != 2029 {
		t.Fatalf("allowed frozen year lost: %#v", metadata.ReleaseYear)
	}
}

func TestScannerRetainsSourceReadCauseAndCancellation(t *testing.T) {
	cause := errors.New("read unavailable")
	memory := newScannerMemory()
	memory.readErr = cause
	if _, err := NewScanner(memory).Scan(t.Context(), 2027); !errors.Is(err, cause) {
		t.Fatalf("lost cause=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewScanner(newScannerMemory()).Scan(ctx, 2027); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation=%v", err)
	}
}

func TestScannerStopsCancellationAtLastIdentityBoundary(t *testing.T) {
	memory := newScannerMemory()
	scanner := NewScanner(memory)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	scanner.newID = func() (string, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return "identity", nil
	}
	result, err := scanner.Scan(ctx, 2027)
	if !errors.Is(
		err,
		context.Canceled,
	) || len(
		result.Items,
	) != 0 {
		t.Fatalf(
			"calls=%d items=%d error=%v",
			calls,
			len(result.Items),
			err,
		)
	}
}
