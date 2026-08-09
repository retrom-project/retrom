package importing

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func writeZIP(t *testing.T, name string, contents []byte) string {
	t.Helper()
	path := t.TempDir() + "/fixture.zip"
	file, err := os.Create(path) //nolint:gosec // Test path is isolated under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create(name)
	if err == nil {
		_, err = entry.Write(contents)
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeLegacyNamedZIP(t *testing.T, name string) string {
	t.Helper()
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(name)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/legacy.zip"
	file, err := os.Create(path) //nolint:gosec // Test path is isolated under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	header := &zip.FileHeader{Name: encoded, NonUTF8: true, Method: zip.Deflate}
	entry, err := archive.CreateHeader(header)
	if err == nil {
		_, err = entry.Write([]byte("fixture-rom"))
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanZIPDecodesLegacyGB18030EntryName(t *testing.T) {
	t.Parallel()
	entries, err := ScanZIP(
		context.Background(),
		writeLegacyNamedZIP(t, "RPG制造.gba"),
		DefaultArchiveLimits(),
	)
	if err != nil || len(entries) != 1 || entries[0].NormalizedPath != "RPG制造.gba" {
		t.Fatalf("ScanZIP() = %#v, error=%v", entries, err)
	}
}

func TestScanZIPValidatesDecodedLegacyPath(t *testing.T) {
	t.Parallel()
	_, err := ScanZIP(
		context.Background(),
		writeLegacyNamedZIP(t, "目录/../game.gba"),
		DefaultArchiveLimits(),
	)
	if !errors.Is(err, ErrUnsafeLogicalPath) {
		t.Fatalf("ScanZIP() error = %v, want %v", err, ErrUnsafeLogicalPath)
	}
}

func TestDOSArchiveLimitsAllowBoundedSparseSavesAndOpaqueNestedData(t *testing.T) {
	t.Parallel()
	sparse := writeZIP(t, "GAME/EMPTY.SAV", bytes.Repeat([]byte{0}, 1<<20))
	if _, err := ScanZIP(context.Background(), sparse, DefaultArchiveLimits()); err != nil {
		t.Fatalf("bounded sparse save rejected: %v", err)
	}
	nested := writeZIP(t, "GAME/DOSBOX/runtime.zip", []byte("PK\x03\x04nested payload"))
	if _, err := ScanZIP(context.Background(), nested, DefaultArchiveLimits()); !errors.Is(err, ErrNestedArchiveUnsupported) {
		t.Fatalf("default nested archive error = %v", err)
	}
	entries, err := ScanZIP(context.Background(), nested, DOSArchiveLimits())
	if err != nil || len(entries) != 1 || entries[0].NormalizedPath != "GAME/DOSBOX/runtime.zip" {
		t.Fatalf("DOS opaque nested data = %#v, error=%v", entries, err)
	}
}

func TestZIPCompressionRatioStillRejectsLargeHighlyCompressedMembers(t *testing.T) {
	t.Parallel()
	large := writeZIP(t, "GAME/large.bin", bytes.Repeat([]byte{0}, (16<<20)+1))
	if _, err := ScanZIP(context.Background(), large, DefaultArchiveLimits()); !errors.Is(err, ErrArchiveLimitExceeded) {
		t.Fatalf("large high-ratio member error = %v", err)
	}
}
