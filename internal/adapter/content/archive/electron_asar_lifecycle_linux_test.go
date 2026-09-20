//go:build linux

package archive

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
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
		entries, err := consumeProject(context.Background(), t, contentprofile.ArchiveElectronASAR, archivePath, importing.DefaultArchiveLimits(),
			func(importing.ArchiveEntry, io.Reader) (importing.ArchiveContent, error) {
				consumerCalls++
				return importing.ArchiveContent{}, importing.ErrArchiveUnsafe
			},
		)
		if !errors.Is(err, importing.ErrElectronASARInvalid) || !errors.Is(err, importing.ErrArchiveUnsafe) {
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
