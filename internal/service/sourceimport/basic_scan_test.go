package sourceimport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestBasicScanFiltersWithoutReadingMetadata(t *testing.T) {
	t.Parallel()
	source := basicSourceFixture()
	result, err := ScanOrganized(t.Context(), source, "BASIC", ".NES;.zip", 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.reads) != 0 || len(result.Metadata) != 0 || len(result.Collections) != 1 ||
		result.Collections[0].GameCount != 2 || len(result.Items) != 2 || result.EstimatedBytes != 10 {
		t.Fatalf("basic projection: %#v; reads:%v", result, source.reads)
	}
	if result.Items[0].Title != "Arcade" || result.Items[1].Files[0].Path != "sub/Game.NES" {
		t.Fatalf("paths or titles changed: %#v", result.Items)
	}
	first := result.Items[1]
	if first.DiscoveryState != "READY" || first.Files[0].Facts != source.files[0].Facts || first.Files[0].Size != 3 {
		t.Fatal("file facts were not frozen")
	}
}

func TestBasicScanEvidenceTracksFactsAndAllFiles(t *testing.T) {
	t.Parallel()
	source := basicSourceFixture()
	result, err := ScanOrganized(t.Context(), source, "BASIC", ".NES;.zip", 2026)
	if err != nil {
		t.Fatal(err)
	}
	first := result.Items[1]
	again, err := ScanOrganized(t.Context(), source, "BASIC", ".zip;.nes;.NES", 2026)
	if err != nil || result.SnapshotDigest != again.SnapshotDigest || first.SourceKey != again.Items[1].SourceKey {
		t.Fatalf("scan evidence is not deterministic: %v", err)
	}
	source.files[0].Facts = strings.Repeat("e", 64)
	changed, err := ScanOrganized(t.Context(), source, "BASIC", ".nes;.zip", 2026)
	if err != nil || changed.SnapshotDigest == result.SnapshotDigest {
		t.Fatal("changed file facts did not change evidence")
	}
	all, err := ScanOrganized(t.Context(), source, "BASIC", "", 2026)
	if err != nil || len(all.Items) != 4 || all.EstimatedBytes != 16 {
		t.Fatal("empty filter excluded files")
	}
}

func TestBasicScanCancellationAndBudget(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := ScanOrganized(ctx, scannerSourceFixture(), "BASIC", "", 2026)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	source := &scanSourceMemory{files: []DiscoveredFile{{Path: "huge.nes", Size: (2 << 40) + 1}}}
	_, err = ScanOrganized(t.Context(), source, "BASIC", "", 2026)
	if !errors.Is(err, ErrScanLimit) {
		t.Fatalf("scan budget lost: %v", err)
	}
	_, err = ScanOrganized(t.Context(), scannerSourceFixture(), "BASIC", ".gba", 2026)
	if !errors.Is(err, ErrFilesAbsent) {
		t.Fatalf("empty selection not reported: %v", err)
	}
}

func TestCreationRejectsInvalidFormatAndExtensionFilters(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ format, input string }{
		{"", ""},
		{"UNKNOWN", ""},
		{"PEGASUS", ".nes"},
		{"GAMELIST", ".zip"},
		{"BASIC", "nes"},
		{"BASIC", "."},
		{"BASIC", "*.nes"},
		{"BASIC", ".nes;"},
		{"BASIC", ".nes;.bad/name"},
		{"BASIC", ".tar.gz"},
		{"BASIC", strings.Repeat("a", 2049)},
	} {
		_, err := NormalizeExtensionFilter(test.format, test.input)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid filter: %#v, %v", test, err)
		}
	}
	value, err := NormalizeExtensionFilter("BASIC", " .ZIP ; .nes; .zip ; .7z; .n-gage ")
	if err != nil || !reflect.DeepEqual(strings.Split(value, ";"), []string{".7z", ".n-gage", ".nes", ".zip"}) {
		t.Fatalf("normalized filter: %q %v", value, err)
	}
}

func basicSourceFixture() *scanSourceMemory {
	return &scanSourceMemory{files: []DiscoveredFile{
		{Path: "sub/Game.NES", Name: "Game.NES", Size: 3, Facts: strings.Repeat("a", 64)},
		{Path: "metadata.pegasus.txt", Name: "metadata.pegasus.txt", Size: 5, Facts: strings.Repeat("b", 64)},
		{Path: "Arcade.zip", Name: "Arcade.zip", Size: 7, Facts: strings.Repeat("c", 64)},
		{Path: "README", Name: "README", Size: 1, Facts: strings.Repeat("d", 64)},
	}}
}
