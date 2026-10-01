package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"retrom/internal/cleanup"
	"retrom/internal/launch"
	application "retrom/internal/service/launch"
)

func TestLaunchBundleBytesAreCanonicalAcrossInputOrder(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	firstMetadata, err := server.contentDeps.Files.Put(bytes.NewReader([]byte("first BIOS")))
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := server.contentDeps.Files.Put(bytes.NewReader([]byte("second BIOS")))
	if err != nil {
		t.Fatal(err)
	}
	files := []launch.BundleFile{
		{LogicalName: "z-bios.bin", FileRecord: secondMetadata.Record, SHA256: secondMetadata.SHA256},
		{LogicalName: "a-bios.bin", FileRecord: firstMetadata.Record, SHA256: firstMetadata.SHA256},
	}
	readBundle := func(input []launch.BundleFile) []byte {
		t.Helper()
		bundle, bundleErr := server.createLaunchBundle(t.Context(), input)
		if bundleErr != nil {
			t.Fatal(bundleErr)
		}
		defer cleanup.Remove(bundle.Name())
		defer func() { cleanup.Error("close", bundle.Close()) }()
		contents, readErr := io.ReadAll(bundle)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return contents
	}
	canonical := readBundle(files)
	reordered := readBundle([]launch.BundleFile{files[1], files[0]})
	if !bytes.Equal(canonical, reordered) {
		t.Fatalf("bundle bytes changed across input order: %x != %x", canonical, reordered)
	}
	metadata, err := launch.NewSources(server.contentDeps.Files, nil).DescribeBundle(t.Context(), []application.ConfigFile{
		{LogicalName: files[0].LogicalName, FileRecord: files[0].FileRecord, Digest: files[0].SHA256},
		{LogicalName: files[1].LogicalName, FileRecord: files[1].FileRecord, Digest: files[1].SHA256},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	if metadata.SizeBytes != int64(len(canonical)) || metadata.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("metadata differs from served ZIP: %#v", metadata)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := launch.WriteDependencyBundle(ctx, io.Discard, files, server.contentDeps.Files.OpenRecord); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled bundle: %v", err)
	}
	files[0].FileRecord = files[1].FileRecord
	if err := launch.WriteDependencyBundle(t.Context(), io.Discard, files, server.contentDeps.Files.OpenRecord); err == nil {
		t.Fatal("changed member bytes retained immutable bundle identity")
	}
}
