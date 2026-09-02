package importing

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScanNWJSExecutableRequiresPEAndAppendedZIP(t *testing.T) {
	t.Parallel()
	valid := writeNWJSExecutable(t, true, true)
	entries, err := ScanNWJSExecutable(context.Background(), valid, DefaultArchiveLimits())
	if err != nil || len(entries) != 1 || entries[0].NormalizedPath != "index.html" {
		t.Fatalf("ScanNWJSExecutable()=%#v, error=%v", entries, err)
	}
	for name, candidate := range map[string]string{
		"zip-renamed.exe": writeNWJSExecutable(t, false, true),
		"pe-only.exe":     writeNWJSExecutable(t, true, false),
	} {
		if _, err := ScanNWJSExecutable(
			context.Background(), candidate, DefaultArchiveLimits(),
		); !errors.Is(err, ErrNWJSExecutableInvalid) {
			t.Fatalf("%s error=%v, want %v", name, err, ErrNWJSExecutableInvalid)
		}
	}
}

func writeNWJSExecutable(t *testing.T, includePE, includeZIP bool) string {
	t.Helper()
	var contents bytes.Buffer
	if includePE {
		pe := make([]byte, 512)
		copy(pe, "MZ")
		pe[0x3c] = 0x80
		copy(pe[0x80:], "PE\x00\x00")
		pe[0x84], pe[0x85], pe[0x86] = 0x4c, 0x01, 0x01
		_, _ = contents.Write(pe)
	}
	if includeZIP {
		archive := zip.NewWriter(&contents)
		entry, err := archive.Create("index.html")
		if err == nil {
			_, err = entry.Write([]byte("<!doctype html>"))
		}
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "game.exe")
	if err := os.WriteFile(path, contents.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
