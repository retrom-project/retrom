package contentprofile

import (
	"errors"
	"testing"

	"retrom/internal/importing"
	"retrom/internal/testassert"
)

func TestProfilesAcceptExactCaseInsensitiveExtensions(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		"nes": {"game.nes", "game.UNIF", "disk.FDS"}, "fds": {"disk.fds"}, "snes": {"game.sfc"},
		"gbc": {"game.gb", "game.GBC"}, "gba": {"game.gba"}, "nds": {"game.nds"},
		"atari5200": {"game.a52"}, "psx": {"game.chd"}, "lynx": {"game.lnx"},
		"saturn": {"game.chd"}, "megadrive": {"game.md"}, "n64": {"game.z64"},
		"3do": {"game.chd"}, "atari7800": {"game.a78"}, "atari2600": {"game.a26"},
		"pce": {"game.pce"}, "pcfx": {"game.chd"}, "ngpc": {"game.ngp"},
		"psp": {"game.iso", "game.CSO"}, "virtualboy": {"game.vb"},
		"wonderswan": {"game.ws", "game.WSC"}, "mastersystem": {"game.sms"},
		"nintendo3ds": {"game.3ds", "game.CCI"},
	}
	for platformID, names := range tests {
		for _, name := range names {
			testassert.CheckTruef(t, AcceptsRaw(platformID, name), "AcceptsRaw(%q, %q) = false", platformID, name)
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
		testassert.CheckFalsef(t, AcceptsRaw(rejected.platform, rejected.name), "AcceptsRaw(%q, %q) = true", rejected.platform, rejected.name)
	}
}

func TestSupportedExtensionsCoverEverySeededPlatformWithoutExposingWrappers(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		"virtualboy": {".vb"}, "wonderswan": {".ws", ".wsc"},
		"mastersystem": {".sms"}, "nintendo3ds": {".3ds", ".cci"},
		"arcade": {".zip"}, "dos": {".exe", ".com", ".bat"},
		"rpgmaker": {".zip", ".7z"},
		"nes":      {".nes", ".unf", ".unif", ".fds"},
	}
	for platformID, want := range tests {
		got := SupportedExtensions(platformID)
		testassert.Falsef(t, len(got) != len(want), "SupportedExtensions(%q) = %#v, want %#v", platformID, got, want)
		for index := range want {
			testassert.Falsef(t, got[index] != want[index], "SupportedExtensions(%q) = %#v, want %#v", platformID, got, want)
		}
		got[0] = ".changed"
		testassert.Falsef(t, SupportedExtensions(platformID)[0] != want[0], "SupportedExtensions(%q) exposed mutable registry storage", platformID)
	}
	if got := SupportedExtensions("unknown"); len(got) != 0 {
		t.Fatalf("unknown platform extensions = %#v", got)
	}
}

func TestSupportedExtensionsContainNoDuplicates(t *testing.T) {
	t.Parallel()
	platformIDs := make([]string, 0, len(registry)+len(specialPlatformExtensions))
	for platformID := range registry {
		platformIDs = append(platformIDs, platformID)
	}
	for platformID := range specialPlatformExtensions {
		platformIDs = append(platformIDs, platformID)
	}
	for _, platformID := range platformIDs {
		seen := make(map[string]struct{})
		for _, extension := range SupportedExtensions(platformID) {
			if _, duplicate := seen[extension]; duplicate {
				t.Errorf("SupportedExtensions(%q) contains duplicate %q", platformID, extension)
			}
			seen[extension] = struct{}{}
		}
	}
}

func TestArchivePoliciesAreExplicitAndReturnedByValue(t *testing.T) {
	t.Parallel()
	profile, ok := ByPlatform("nds")
	testassert.Falsef(t, testassert.Any(func() bool { return !ok }, func() bool { return profile.ArchivePolicy != ArchiveSinglePrimary }, func() bool { return !AcceptsArchive("nds", ArchiveZIP) }, func() bool { return !AcceptsArchive("nds", ArchiveSevenZip) }), "NDS profile = %#v", profile)
	profile.Extensions[0] = ".changed"
	profile.ArchiveFormats[0] = "CHANGED"
	profile.ContentKinds[0] = "CHANGED"
	testassert.False(t, testassert.Any(func() bool { return !AcceptsRaw("nds", "game.nds") }, func() bool { return !AcceptsArchive("nds", ArchiveZIP) }), "ByPlatform exposed mutable registry storage")
	for _, platformID := range []string{"psx", "saturn", "3do", "pcfx", "psp"} {
		profile, ok := ByPlatform(platformID)
		testassert.CheckFalsef(t, testassert.Any(func() bool { return !ok }, func() bool { return profile.ArchivePolicy != ArchiveNone }, func() bool { return AcceptsArchive(platformID, ArchiveZIP) }, func() bool { return AcceptsArchive(platformID, ArchiveSevenZip) }), "raw-only profile %q = %#v", platformID, profile)
	}
}

func TestMultiDiscContentKindIsExplicitlyLimitedToSaturn(t *testing.T) {
	t.Parallel()
	for platformID := range registry {
		got := AllowsContentKind(platformID, ContentKindMultiDiscM3UV1)
		testassert.CheckFalsef(t, got != (platformID == "saturn"), "AllowsContentKind(%q, MULTI_DISC_M3U_V1) = %t", platformID, got)
		switch platformID {
		case "rpgmaker":
			testassert.CheckTruef(t, AllowsContentKind(platformID, ContentKindRPGMakerProject), "RPG Maker project support missing")
			testassert.CheckFalsef(t, AllowsContentKind(platformID, ContentKindSingleFile), "RPG Maker accepted SINGLE_FILE")
		case "ons":
			testassert.CheckTruef(t, AllowsContentKind(platformID, ContentKindONSProject), "ONS project support missing")
			testassert.CheckFalsef(t, AllowsContentKind(platformID, ContentKindSingleFile), "ONS accepted SINGLE_FILE")
		case "kirikiri":
			testassert.CheckTruef(t, AllowsContentKind(platformID, ContentKindKiriKiriProject), "KiriKiri project support missing")
			testassert.CheckFalsef(t, AllowsContentKind(platformID, ContentKindSingleFile), "KiriKiri accepted SINGLE_FILE")
		default:
			testassert.CheckTruef(t, AllowsContentKind(platformID, ContentKindSingleFile), "platform %q lost SINGLE_FILE support", platformID)
		}
	}
	testassert.False(t, AllowsContentKind("unknown", ContentKindMultiDiscM3UV1), "unknown platform accepted multi-disc content")
}

func TestONSProfileAcceptsOnlyProjectArchiveTransport(t *testing.T) {
	t.Parallel()
	profile, ok := ByPlatform("ons")
	testassert.Falsef(t, testassert.Any(
		func() bool { return !ok },
		func() bool { return profile.ArchivePolicy != ArchiveProject },
		func() bool { return !AcceptsArchive("ons", ArchiveZIP) },
		func() bool { return !AcceptsArchive("ons", ArchiveSevenZip) },
		func() bool { return AcceptsRaw("ons", "game.zip") },
	), "ONS profile=%#v", profile)
}

func TestKiriKiriProfileAcceptsOnlyProjectArchiveTransport(t *testing.T) {
	t.Parallel()
	profile, ok := ByPlatform("kirikiri")
	testassert.Falsef(t, testassert.Any(
		func() bool { return !ok },
		func() bool { return profile.ArchivePolicy != ArchiveProject },
		func() bool { return !AcceptsArchive("kirikiri", ArchiveZIP) },
		func() bool { return !AcceptsArchive("kirikiri", ArchiveSevenZip) },
		func() bool { return AcceptsRaw("kirikiri", "game.zip") },
	), "KiriKiri profile=%#v", profile)
}

func TestRPGMakerProfileAcceptsOnlyProjectArchiveTransport(t *testing.T) {
	profile, ok := ByPlatform("rpgmaker")
	testassert.Falsef(t, testassert.Any(
		func() bool { return !ok },
		func() bool { return profile.ArchivePolicy != ArchiveProject },
		func() bool { return !AcceptsArchive("rpgmaker", ArchiveZIP) },
		func() bool { return !AcceptsArchive("rpgmaker", ArchiveSevenZip) },
		func() bool { return AcceptsRaw("rpgmaker", "game.zip") },
	), "RPG Maker profile=%#v", profile)
}

func TestSelectArchivePrimary(t *testing.T) {
	t.Parallel()
	entries := make([]importing.ArchiveEntry, 0, 3)
	entries = append(entries,
		importing.ArchiveEntry{Ordinal: 0, NormalizedPath: "README.txt"},
		importing.ArchiveEntry{Ordinal: 1, NormalizedPath: "folder/Game.NDS"},
	)
	selected, err := SelectArchivePrimary("nds", entries)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return selected.Ordinal != 1 }), "SelectArchivePrimary() = %#v, %v", selected, err)
	if _, err := SelectArchivePrimary("nds", entries[:1]); !errors.Is(err, ErrNoSupportedContent) {
		t.Fatalf("zero candidates error = %v", err)
	}
	entries = append(entries, importing.ArchiveEntry{Ordinal: 2, NormalizedPath: "other.nds"})
	if _, err := SelectArchivePrimary("nds", entries); !errors.Is(err, ErrAmbiguousPrimaryContent) {
		t.Fatalf("multiple candidates error = %v", err)
	}
}
