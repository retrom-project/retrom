//go:build linux

package importing

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestElectronASARZIPClosesUnrecognizedArchive(t *testing.T) {
	// Keep finalizers from hiding a missing Close during this bounded, serial test.
	previousGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGCPercent)

	archivePath := writeElectronASARZIP(t, map[string][]byte{"index.html": []byte("ok")}, nil, false)
	if count := electronArchiveOpenDescriptors(t, archivePath); count != 0 {
		t.Fatalf("fixture has %d open descriptors before scanning", count)
	}

	consumerCalls := 0
	for attempt := range 2 {
		entries, err := ScanElectronASARZIPWithConsumer(
			context.Background(), archivePath, DefaultArchiveLimits(),
			func(ArchiveEntry, io.Reader) (ArchiveContent, error) {
				consumerCalls++
				return ArchiveContent{}, ErrArchiveUnsafe
			},
		)
		if !errors.Is(err, ErrElectronASARInvalid) || !errors.Is(err, ErrArchiveUnsafe) {
			t.Fatalf("scan %d error = %v, want the ASAR and archive sentinels", attempt, err)
		}
		if err.Error() != "ARCHIVE_UNSAFE: ELECTRON_ASAR_INVALID" || entries != nil || consumerCalls != 0 {
			t.Fatalf("scan %d changed rejection: entries=%v error=%v consumer calls=%d",
				attempt, entries, err, consumerCalls)
		}
		if count := electronArchiveOpenDescriptors(t, archivePath); count != 0 {
			t.Errorf("scan %d left %d descriptors open for rejected archive", attempt, count)
		}
	}
}

func electronArchiveOpenDescriptors(t *testing.T, archivePath string) int {
	t.Helper()
	descriptors, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, descriptor := range descriptors {
		target, readErr := os.Readlink(filepath.Join("/proc/self/fd", descriptor.Name()))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if target == archivePath {
			count++
		}
	}
	return count
}
