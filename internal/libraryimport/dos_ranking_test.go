package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"testing"

	"retrom/internal/blobstore"
	"retrom/internal/testassert"
)

func TestDOSRankingPromotesGameAfterInteractiveLauncherHelper(t *testing.T) {
	t.Parallel()
	entries := []preparedDOSEntry{
		{path: "PAL/PLAY.BAT", kind: "BAT", safe: true, batchContents: []byte("@echo\r\nJS3 PAL.JS3\r\nPAL\r\n")},
		{path: "PAL/JS3.EXE", kind: "EXE", safe: true},
		{path: "PAL/PAL.EXE", kind: "EXE", safe: true},
		{path: "PAL/INSTALL.EXE", kind: "EXE", safe: true},
	}

	rankDOSEntries(entries)

	testassert.Falsef(t, testassert.Any(func() bool { return entries[0].path != "PAL/PAL.EXE" }, func() bool { return entries[0].rank != 0 }, func() bool { return !entries[0].inferredTerminalTarget }), "interactive launcher default = %#v", entries)
	testassert.Falsef(t, testassert.Any(func() bool { return entries[1].path != "PAL/PLAY.BAT" }, func() bool { return entries[2].path != "PAL/INSTALL.EXE" }, func() bool { return entries[3].path != "PAL/JS3.EXE" }), "interactive launcher candidates = %#v", entries)
}

func TestDOSRankingKeepsLauncherWhenBatchHasNoInteractiveHelper(t *testing.T) {
	t.Parallel()
	entries := []preparedDOSEntry{
		{path: "GAME/PLAY.BAT", kind: "BAT", safe: true, batchContents: []byte("@echo off\r\nSET BLASTER=A220 I7 D1\r\nMAIN.EXE\r\n")},
		{path: "GAME/MAIN.EXE", kind: "EXE", safe: true},
	}

	rankDOSEntries(entries)

	testassert.Falsef(t, testassert.Any(func() bool { return entries[0].path != "GAME/PLAY.BAT" }, func() bool { return entries[0].rank != 0 }, func() bool { return entries[1].inferredTerminalTarget }), "non-interactive launcher default = %#v", entries)
}

func TestDOSRankingFailsClosedForConditionalUnknownAndOversizedBatch(t *testing.T) {
	t.Parallel()
	tests := map[string][]byte{
		"conditional": []byte("IF EXIST PAL.JS3 JS3 PAL.JS3\r\nPAL\r\n"),
		"unknown":     []byte("CLS\r\nJS3 PAL.JS3\r\nPAL\r\n"),
		"oversized":   bytes.Repeat([]byte(" "), maxDOSBatchInspectionBytes+1),
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			entries := []preparedDOSEntry{
				{path: "PAL/PLAY.BAT", kind: "BAT", safe: true, batchContents: contents},
				{path: "PAL/JS3.EXE", kind: "EXE", safe: true},
				{path: "PAL/PAL.EXE", kind: "EXE", safe: true},
			}
			rankDOSEntries(entries)
			testassert.Falsef(t, testassert.Any(func() bool { return entries[0].path != "PAL/PLAY.BAT" }, func() bool { return entries[1].inferredTerminalTarget }, func() bool { return entries[2].inferredTerminalTarget }), "%s batch was inferred: %#v", name, entries)
		})
	}
}

func TestPrepareDOSFilesInspectsLauncherBatchForDirectoryAndZIP(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	service := (&Service{}).WithBlobStore(blobs)
	files := map[string][]byte{
		"PAL/PLAY.BAT": []byte("@echo\r\nJS3 PAL.JS3\r\nPAL\r\n"),
		"PAL/JS3.EXE":  []byte("helper"),
		"PAL/PAL.EXE":  []byte("game"),
	}
	directorySources := make([]importSourceFile, 0, len(files))
	for path, contents := range files {
		metadata, putErr := blobs.Put(bytes.NewReader(contents))
		testassert.False(t, putErr != nil, putErr)
		directorySources = append(directorySources, importSourceFile{
			id: path, path: path, blobID: "blob-" + path, sha256: metadata.SHA256, size: metadata.Size,
		})
	}
	_, directoryGroups, _ := service.prepareDOSFiles(context.Background(), "DIRECTORY", directorySources)
	testassert.Falsef(t, testassert.Any(func() bool { return len(directoryGroups) != 1 }, func() bool { return directoryGroups[0].defaultDOSEntry != "PAL/PAL.EXE" }), "directory DOS default = %#v", directoryGroups)

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{"PAL/PLAY.BAT", "PAL/JS3.EXE", "PAL/PAL.EXE"} {
		entry, createErr := writer.Create(name)
		testassert.False(t, createErr != nil, createErr)
		if _, writeErr := entry.Write(files[name]); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	archiveMetadata, err := blobs.Put(bytes.NewReader(archive.Bytes()))
	testassert.False(t, err != nil, err)
	archiveSource := importSourceFile{
		id: "archive", path: "pal.zip", blobID: "archive-blob", sha256: archiveMetadata.SHA256,
		size: archiveMetadata.Size,
	}
	_, archiveGroups, _ := service.prepareDOSFiles(context.Background(), "FILES", []importSourceFile{archiveSource})
	testassert.Falsef(t, testassert.Any(func() bool { return len(archiveGroups) != 1 }, func() bool { return archiveGroups[0].defaultDOSEntry != "PAL/PAL.EXE" }), "ZIP DOS default = %s / %#v", fmt.Sprint(len(archiveGroups)), archiveGroups)
}
