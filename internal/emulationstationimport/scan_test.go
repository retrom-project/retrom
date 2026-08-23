package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"retrom/internal/emulationstationmeta"
	"retrom/internal/multidisc"
	"retrom/internal/testassert"
)

func TestScanProjectsMultipleGamelistsWithoutGuessingPlatforms(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	writeScanFile(t, rootPath, "nes/first.nes", []byte("NES"))
	writeScanFile(t, rootPath, "pce/second.pce", []byte("PCE"))
	writeScanFile(t, rootPath, "nes/gamelist.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<gameList><provider>ignored</provider><folder><path>./folder</path></folder><game>
<path>./first.nes</path><name>First</name><hidden>true</hidden></game></gameList>`))
	writeScanFile(t, rootPath, "pce/gamelist.xml", []byte(
		`<gameList><game><path>.\second.pce</path><name>Second</name><adult>true</adult></game></gameList>`,
	))
	writeScanFile(t, rootPath, "ignored/Gamelist.xml", []byte(`<gameList/>`))

	result, err := (&Service{}).scan(context.Background(), Root{path: rootPath}, "", 2027)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, len(result.Gamelists) != 2, "gamelists = %d", len(result.Gamelists))
	testassert.Falsef(t, len(result.Collections) != 2, "collections = %d", len(result.Collections))
	testassert.Falsef(t, len(result.Items) != 2, "items = %d", len(result.Items))
	testassert.Falsef(t, result.FolderEntries != 1, "folders = %d", result.FolderEntries)
	testassert.Falsef(t, result.Collections[0].DisplayName != "nes", "collection = %#v", result.Collections[0])
	testassert.Falsef(t, result.Collections[0].HiddenGameCount != 1, "collection = %#v", result.Collections[0])
	testassert.Falsef(t, result.Collections[1].AdultGameCount != 1, "collection = %#v", result.Collections[1])
	testassert.Falsef(t, result.Items[0].Files[0].Path != "nes/first.nes", "item = %#v", result.Items[0])
	testassert.Falsef(t, result.Items[0].SourceKey == result.Items[1].SourceKey, "source keys collided")
	testassert.Falsef(t, len(result.SnapshotDigest) != 64, "snapshot digest = %q", result.SnapshotDigest)
}

func TestScanIsolatesInvalidGamelistAndRejectsAllInvalid(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	writeScanFile(t, rootPath, "valid/game.gba", []byte("GBA"))
	writeScanFile(t, rootPath, "valid/gamelist.xml", []byte(
		`<gameList><game><path>./game.gba</path><name>Game</name></game></gameList>`,
	))
	writeScanFile(t, rootPath, "broken/gamelist.xml", []byte(`<gameList><game>`))
	result, err := (&Service{}).scan(context.Background(), Root{path: rootPath}, "", 2027)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, result.InvalidGamelists != 1, "invalid = %d", result.InvalidGamelists)
	testassert.Falsef(t, len(result.Items) != 1, "items = %d", len(result.Items))

	onlyInvalid := t.TempDir()
	writeScanFile(t, onlyInvalid, "gamelist.xml", []byte(`<gameList><game>`))
	_, err = (&Service{}).scan(context.Background(), Root{path: onlyInvalid}, "", 2027)
	testassert.Truef(t, errors.Is(err, ErrNoValidGamelist), "error = %v", err)
}

func TestScanIsolatesOversizedGamelistWithoutReadingItsContents(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	writeScanFile(t, rootPath, "valid/game.gba", []byte("GBA"))
	writeScanFile(t, rootPath, "valid/gamelist.xml", []byte(
		`<gameList><game><path>./game.gba</path><name>Game</name></game></gameList>`,
	))
	oversizedPath := filepath.Join(rootPath, "oversized", "gamelist.xml")
	if err := os.MkdirAll(filepath.Dir(oversizedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Create(oversizedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Truncate(maxGamelistBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := (&Service{}).scan(context.Background(), Root{path: rootPath}, "", 2027)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, len(result.Gamelists) != 2, "gamelists = %#v", result.Gamelists)
	testassert.Falsef(t, result.InvalidGamelists != 1, "invalid = %d", result.InvalidGamelists)
	testassert.Falsef(t, len(result.Items) != 1, "items = %#v", result.Items)
	oversized := result.Gamelists[0]
	if oversized.Path != "oversized/gamelist.xml" {
		oversized = result.Gamelists[1]
	}
	testassert.Falsef(t, oversized.State != "INVALID", "oversized = %#v", oversized)
	testassert.Falsef(t,
		oversized.ErrorCode != emulationstationmeta.ErrTooLarge.Error(),
		"oversized = %#v", oversized,
	)
	testassert.Falsef(t, oversized.Digest != "", "oversized digest = %q", oversized.Digest)
}

func TestScanFreezesM3UAndPresentDiscs(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	writeScanFile(t, rootPath, "disc/game.m3u", []byte("one.chd\ntwo.chd\n"))
	writeScanFile(t, rootPath, "disc/one.chd", append([]byte("MComprHD"), []byte("one")...))
	writeScanFile(t, rootPath, "disc/two.chd", append([]byte("MComprHD"), []byte("two")...))
	writeScanFile(t, rootPath, "disc/unreferenced.chd", []byte("bad"))
	writeScanFile(t, rootPath, "disc/gamelist.xml", []byte(
		`<gameList><game><path>./game.m3u</path><name>Multi</name></game></gameList>`,
	))
	result, err := (&Service{}).scan(context.Background(), Root{path: rootPath}, "", 2027)
	testassert.False(t, err != nil, err)
	item := result.Items[0]
	testassert.Falsef(t, item.ContentKind != multidisc.ContentKind, "content kind = %q", item.ContentKind)
	testassert.Falsef(t, len(item.Files) != 3, "files = %#v", item.Files)
	testassert.Falsef(t, item.Files[0].Kind != "PLAYLIST", "files = %#v", item.Files)
	testassert.Falsef(t, item.Files[1].Kind != "DISC" || item.Files[2].Kind != "DISC", "files = %#v", item.Files)
}

func TestReferencedDiscPathsExcludeUnrelatedCHDs(t *testing.T) {
	t.Parallel()
	exact := map[string]string{
		"one.chd":    "disc/one.chd",
		"TWO.CHD":    "disc/TWO.CHD",
		"unused.chd": "disc/unused.chd",
	}
	folded := map[string][]string{
		"one.chd":    {"disc/one.chd"},
		"two.chd":    {"disc/TWO.CHD"},
		"unused.chd": {"disc/unused.chd"},
	}
	paths := referencedDiscPaths([]string{"one.chd", "two.chd", "missing.chd"}, exact, folded)
	sort.Strings(paths)
	if got, want := strings.Join(paths, "|"), "disc/TWO.CHD|disc/one.chd"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestSnapshotDigestUsesTheSpecifiedCanonicalFieldOrder(t *testing.T) {
	t.Parallel()
	gamelists := []scannedGamelist{{
		Path: "nes/gamelist.xml", Size: 42, Digest: "content",
		Facts: "facts", State: "VALID",
	}}
	canonical := []byte(
		`{"schemaVersion":1,"gamelists":[{"path":"nes/gamelist.xml","sizeBytes":42,` +
			`"contentDigest":"content","factsDigest":"facts","parseState":"VALID"}]}`,
	)
	want := sha256.Sum256(canonical)
	testassert.Falsef(t, snapshotDigest(gamelists) != hex.EncodeToString(want[:]),
		"snapshot digest did not use the documented canonical JSON")
}

func TestBlockedGameManifestUsesAnEmptyFilesArray(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	writeScanFile(t, rootPath, "gamelist.xml", []byte(
		`<gameList><game><name>Missing path</name></game></gameList>`,
	))
	result, err := (&Service{}).scan(context.Background(), Root{path: rootPath}, "", 2027)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, len(result.Items) != 1, "items = %d", len(result.Items))
	testassert.Falsef(t,
		result.Items[0].SourceManifestJSON !=
			`{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[]}`,
		"manifest = %s", result.Items[0].SourceManifestJSON,
	)
}

func TestMediaProjectionMatchesCoverPathKind(t *testing.T) {
	t.Parallel()
	warnings := []map[string]any{{
		"code": "EMULATIONSTATION_MEDIA_MISSING", "field": "image", "pathKind": "COVER",
	}}
	testassert.Falsef(t, mediaProjection(false, warnings, "cover") != "WARNING", "cover warning was lost")
	testassert.Falsef(t, mediaProjection(false, warnings, "video") != "MISSING", "cover warning leaked to video")
}

func writeScanFile(t *testing.T, rootPath, relativePath string, contents []byte) {
	t.Helper()
	fullPath := filepath.Join(rootPath, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}
