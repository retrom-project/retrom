package pegasusimport

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanProjectsExplicitFilesAndMediaFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, directory := range []string{"library/roms", "library/media/Test Game"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	metadata := "collection: Nintendo\nshortname: nes\nextensions: nes\nlaunch: must-not-be-saved\ngame: Test Game\nfile: roms\\test.nes\nassets.boxfront: missing.png\nassets.video: bad.wmv\n"
	writeFixture(t, filepath.Join(root, "library/metadata.pegasus.txt"), []byte(metadata))
	writeFixture(t, filepath.Join(root, "library/roms/test.nes"), []byte("deterministic-rom-fixture"))
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "library/media/Test Game/BOXFRONT.PNG"), pngBytes)
	mp4 := []byte{
		0,
		0,
		0,
		24,
		'f',
		't',
		'y',
		'p',
		'i',
		's',
		'o',
		'm',
		0,
		0,
		0,
		0,
		'i',
		's',
		'o',
		'm',
		'm',
		'p',
		'4',
		'2',
	}
	writeFixture(t, filepath.Join(root, "library/media/Test Game/video.mp4"), mp4)

	result, err := (&Service{}).scan(context.Background(), Root{path: root}, "library")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata) != 1 || len(result.Collections) != 1 || len(result.Items) != 1 {
		t.Fatalf(
			"scan counts = metadata:%d collections:%d items:%d",
			len(result.Metadata),
			len(result.Collections),
			len(result.Items),
		)
	}
	item := result.Items[0]
	if item.DiscoveryState != "READY" || len(item.Files) != 1 || item.Files[0].Path != "roms/test.nes" {
		t.Fatalf("item = %#v", item)
	}
	if len(item.Assets) != 2 || item.Assets[0].Kind != "COVER" || item.Assets[0].Method != "AUTO_TITLE" ||
		item.Assets[1].Kind != "VIDEO" {
		t.Fatalf("assets = %#v", item.Assets)
	}
	if result.Covers != 1 || result.Videos != 1 || result.MediaWarnings != 2 {
		t.Fatalf("media counts = covers:%d videos:%d warnings:%d", result.Covers, result.Videos, result.MediaWarnings)
	}
	if strings.Contains(item.MetadataJSON, "must-not-be-saved") ||
		strings.Contains(item.WarningsJSON, "must-not-be-saved") ||
		strings.Contains(item.SourceManifestJSON, "must-not-be-saved") {
		t.Fatal("launch command leaked into persisted projection")
	}
}

func TestScanBlocksASCIICasefoldSourceCollision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "library"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(
		t,
		filepath.Join(root, "library/metadata.pegasus.txt"),
		[]byte("collection: Test\ngame: Collision\nfile: game.nes\n"),
	)
	writeFixture(t, filepath.Join(root, "library/game.nes"), []byte("one"))
	writeFixture(t, filepath.Join(root, "library/GAME.NES"), []byte("two"))
	result, err := (&Service{}).scan(context.Background(), Root{path: root}, "library")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].DiscoveryState != "BLOCKED_SOURCE" ||
		result.Items[0].DiscoveryCode != "PEGASUS_PATH_INVALID" {
		t.Fatalf("collision item = %#v", result.Items)
	}
}

func TestScanSourceKeyIsStableAndBoundsBlockedFileProjection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "library"), 0o700); err != nil {
		t.Fatal(err)
	}
	var metadata strings.Builder
	metadata.WriteString("collection: Test\ngame: Too many files\n")
	for ordinal := 0; ordinal <= 64; ordinal++ {
		name := fmt.Sprintf("disc-%02d.chd", ordinal)
		metadata.WriteString("file: " + name + "\n")
		writeFixture(t, filepath.Join(root, "library", name), []byte(name))
	}
	writeFixture(t, filepath.Join(root, "library/metadata.pegasus.txt"), []byte(metadata.String()))

	first, err := (&Service{}).scan(context.Background(), Root{path: root}, "library")
	if err != nil {
		t.Fatal(err)
	}
	second, err := (&Service{}).scan(context.Background(), Root{path: root}, "library")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || len(first.Items[0].Files) != 64 {
		t.Fatalf("bounded item files = %#v", first.Items)
	}
	if first.Items[0].DiscoveryCode != "PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED" {
		t.Fatalf("discovery code = %q", first.Items[0].DiscoveryCode)
	}
	if first.Items[0].SourceKey != second.Items[0].SourceKey {
		t.Fatalf("source key changed across scans: %q != %q", first.Items[0].SourceKey, second.Items[0].SourceKey)
	}
}

func writeFixture(t *testing.T, name string, value []byte) {
	t.Helper()
	if err := os.WriteFile(name, value, 0o600); err != nil {
		t.Fatal(err)
	}
}
