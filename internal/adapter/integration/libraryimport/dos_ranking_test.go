package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/testkit/testassert"
)

func TestDOSRankingPromotesGameAfterInteractiveLauncherHelper(t *testing.T) {
	t.Parallel()
	entries := []preparedDOSEntry{
		{Path: "PAL/PLAY.BAT", Kind: "BAT", Safe: true, BatchContents: []byte("@echo\r\nJS3 PAL.JS3\r\nPAL\r\n")},
		{Path: "PAL/JS3.EXE", Kind: "EXE", Safe: true},
		{Path: "PAL/PAL.EXE", Kind: "EXE", Safe: true},
		{Path: "PAL/INSTALL.EXE", Kind: "EXE", Safe: true},
	}

	rankDOSEntries(entries)

	testassert.Falsef(t, testassert.Any(func() bool { return entries[0].Path != "PAL/PAL.EXE" }, func() bool { return entries[0].Rank != 0 }, func() bool { return !entries[0].InferredTerminalTarget }), "interactive launcher default = %#v", entries)
	testassert.Falsef(t, testassert.Any(func() bool { return entries[1].Path != "PAL/PLAY.BAT" }, func() bool { return entries[2].Path != "PAL/INSTALL.EXE" }, func() bool { return entries[3].Path != "PAL/JS3.EXE" }), "interactive launcher candidates = %#v", entries)
}

func TestDOSRankingKeepsLauncherWhenBatchHasNoInteractiveHelper(t *testing.T) {
	t.Parallel()
	entries := []preparedDOSEntry{
		{Path: "GAME/PLAY.BAT", Kind: "BAT", Safe: true, BatchContents: []byte("@echo off\r\nSET BLASTER=A220 I7 D1\r\nMAIN.EXE\r\n")},
		{Path: "GAME/MAIN.EXE", Kind: "EXE", Safe: true},
	}

	rankDOSEntries(entries)

	testassert.Falsef(t, testassert.Any(func() bool { return entries[0].Path != "GAME/PLAY.BAT" }, func() bool { return entries[0].Rank != 0 }, func() bool { return entries[1].InferredTerminalTarget }), "non-interactive launcher default = %#v", entries)
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
				{Path: "PAL/PLAY.BAT", Kind: "BAT", Safe: true, BatchContents: contents},
				{Path: "PAL/JS3.EXE", Kind: "EXE", Safe: true},
				{Path: "PAL/PAL.EXE", Kind: "EXE", Safe: true},
			}
			rankDOSEntries(entries)
			testassert.Falsef(t, testassert.Any(func() bool { return entries[0].Path != "PAL/PLAY.BAT" }, func() bool { return entries[1].InferredTerminalTarget }, func() bool { return entries[2].InferredTerminalTarget }), "%s batch was inferred: %#v", name, entries)
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
			ID: path, Path: path, BlobID: "blob-" + path, SHA256: metadata.SHA256, Size: metadata.Size,
		})
	}
	_, directoryGroups, _ := service.prepareDOSFiles(context.Background(), "DIRECTORY", directorySources)
	testassert.Falsef(t, testassert.Any(func() bool { return len(directoryGroups) != 1 }, func() bool { return directoryGroups[0].DefaultDOSEntry != "PAL/PAL.EXE" }), "directory DOS default = %#v", directoryGroups)

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
		ID: "archive", Path: "pal.zip", BlobID: "archive-blob", SHA256: archiveMetadata.SHA256,
		Size: archiveMetadata.Size,
	}
	_, archiveGroups, _ := service.prepareDOSFiles(context.Background(), "FILES", []importSourceFile{archiveSource})
	testassert.Falsef(t, testassert.Any(func() bool { return len(archiveGroups) != 1 }, func() bool { return archiveGroups[0].DefaultDOSEntry != "PAL/PAL.EXE" }), "ZIP DOS default = %s / %#v", fmt.Sprint(len(archiveGroups)), archiveGroups)
}
