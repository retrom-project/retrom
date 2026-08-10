package contentprofile

import (
	"errors"
	"testing"

	"retrom/internal/importing"
)

func TestProfilesAcceptExactCaseInsensitiveExtensions(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		"nes": {"game.nes", "game.UNIF"}, "fds": {"disk.fds"}, "snes": {"game.sfc"},
		"gbc": {"game.gb", "game.GBC"}, "gba": {"game.gba"}, "nds": {"game.nds"},
		"atari5200": {"game.a52"}, "psx": {"game.chd"}, "lynx": {"game.lnx"},
		"saturn": {"game.chd"}, "megadrive": {"game.md"}, "n64": {"game.z64"},
		"3do": {"game.chd"}, "atari7800": {"game.a78"}, "atari2600": {"game.a26"},
		"pce": {"game.pce"}, "pcfx": {"game.chd"}, "ngpc": {"game.ngp"},
		"psp": {"game.iso", "game.CSO"},
	}
	for platformID, names := range tests {
		for _, name := range names {
			if !AcceptsRaw(platformID, name) {
				t.Errorf("AcceptsRaw(%q, %q) = false", platformID, name)
			}
		}
	}
	for _, rejected := range []struct{ platform, name string }{
		{"psp", "game.chd"},
		{"psp", "game.iso.7z"},
		{"psx", "game.iso"},
		{"n64", "game.v64"},
		{"arcade", "game.zip"},
		{"unknown", "game.gba"},
	} {
		if AcceptsRaw(rejected.platform, rejected.name) {
			t.Errorf("AcceptsRaw(%q, %q) = true", rejected.platform, rejected.name)
		}
	}
}

func TestArchivePoliciesAreExplicitAndReturnedByValue(t *testing.T) {
	t.Parallel()
	profile, ok := ByPlatform("nds")
	if !ok || profile.ArchivePolicy != ArchiveSinglePrimary ||
		!AcceptsArchive("nds", ArchiveZIP) || !AcceptsArchive("nds", ArchiveSevenZip) {
		t.Fatalf("NDS profile = %#v", profile)
	}
	profile.Extensions[0] = ".changed"
	profile.ArchiveFormats[0] = "CHANGED"
	profile.ContentKinds[0] = "CHANGED"
	if !AcceptsRaw("nds", "game.nds") || !AcceptsArchive("nds", ArchiveZIP) {
		t.Fatal("ByPlatform exposed mutable registry storage")
	}
	for _, platformID := range []string{"psx", "saturn", "3do", "pcfx", "psp"} {
		profile, ok := ByPlatform(platformID)
		if !ok || profile.ArchivePolicy != ArchiveNone || AcceptsArchive(platformID, ArchiveZIP) ||
			AcceptsArchive(platformID, ArchiveSevenZip) {
			t.Errorf("raw-only profile %q = %#v", platformID, profile)
		}
	}
}

func TestMultiDiscContentKindIsExplicitlyLimitedToSaturn(t *testing.T) {
	t.Parallel()
	for platformID := range registry {
		got := AllowsContentKind(platformID, ContentKindMultiDiscM3UV1)
		if got != (platformID == "saturn") {
			t.Errorf("AllowsContentKind(%q, MULTI_DISC_M3U_V1) = %t", platformID, got)
		}
		if !AllowsContentKind(platformID, ContentKindSingleFile) {
			t.Errorf("platform %q lost SINGLE_FILE support", platformID)
		}
	}
	if AllowsContentKind("unknown", ContentKindMultiDiscM3UV1) {
		t.Fatal("unknown platform accepted multi-disc content")
	}
}

func TestSelectArchivePrimary(t *testing.T) {
	t.Parallel()
	entries := make([]importing.ArchiveEntry, 0, 3)
	entries = append(entries,
		importing.ArchiveEntry{Ordinal: 0, NormalizedPath: "README.txt"},
		importing.ArchiveEntry{Ordinal: 1, NormalizedPath: "folder/Game.NDS"},
	)
	selected, err := SelectArchivePrimary("nds", entries)
	if err != nil || selected.Ordinal != 1 {
		t.Fatalf("SelectArchivePrimary() = %#v, %v", selected, err)
	}
	if _, err := SelectArchivePrimary("nds", entries[:1]); !errors.Is(err, ErrNoSupportedContent) {
		t.Fatalf("zero candidates error = %v", err)
	}
	entries = append(entries, importing.ArchiveEntry{Ordinal: 2, NormalizedPath: "other.nds"})
	if _, err := SelectArchivePrimary("nds", entries); !errors.Is(err, ErrAmbiguousPrimaryContent) {
		t.Fatalf("multiple candidates error = %v", err)
	}
}
