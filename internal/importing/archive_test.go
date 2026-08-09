package importing

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

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
