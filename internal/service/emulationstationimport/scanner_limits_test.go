package emulationstationimport

import (
	"errors"
	"fmt"
	"testing"

	"retrom/internal/capability/format/emulationstationmeta"
)

func TestScannerIsolatesOversizedMetadataWithoutReadingBytes(t *testing.T) {
	memory := newScannerMemory()
	memory.files = append(
		memory.files,
		DiscoveredFile{Path: "oversized/gamelist.xml", Name: "gamelist.xml", Facts: "oversized", Size: maxGamelistBytes + 1},
	)
	result, err := NewScanner(memory).Scan(t.Context(), 2027)
	if err != nil || len(result.Items) != 1 || result.InvalidGamelists != 1 || len(memory.reads) != 1 {
		t.Fatalf("result=%#v reads=%v error=%v", result, memory.reads, err)
	}
	oversized := result.Gamelists[1]
	if oversized.Digest != "" || oversized.ErrorCode != emulationstationmeta.ErrTooLarge.Error() {
		t.Fatalf("oversized=%#v", oversized)
	}
}

func TestScannerEnforcesGamelistCountAndAggregateByteBoundaries(t *testing.T) {
	for _, limit := range []string{"count", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			index := scanIndex{ctx: t.Context(), files: map[string]discoveredFile{}}
			count, size := maxGamelists, int64(1)
			if limit == "bytes" {
				count = maxGamelistsBytes / int(maxGamelistBytes)
				size = maxGamelistBytes
			}
			for n := range count {
				if err := index.visit(
					DiscoveredFile{Path: fmt.Sprintf("%d/gamelist.xml", n), Name: "gamelist.xml", Size: size},
				); err != nil {
					t.Fatalf("premature limit %d: %v", n, err)
				}
			}
			if err := index.visit(
				DiscoveredFile{Path: "overflow/gamelist.xml", Name: "gamelist.xml", Size: size},
			); !errors.Is(
				err,
				ErrScanLimit,
			) {
				t.Fatalf("accepted overflow: %v", err)
			}
		})
	}
}

func TestScannerAllInvalidRetainsDiagnosticProjection(t *testing.T) {
	memory := newScannerMemory()
	memory.data["gamelist.xml"] = []byte("<gameList><game>")
	result, err := NewScanner(memory).Scan(t.Context(), 2027)
	if !errors.Is(
		err,
		ErrNoValidGamelist,
	) || result.InvalidGamelists != 1 || len(
		result.Gamelists,
	) != 1 || result.SnapshotDigest == "" || len(
		result.Items,
	) != 0 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}
