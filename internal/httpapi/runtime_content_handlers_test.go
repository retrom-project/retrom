package httpapi

import (
	"bytes"
	"io"
	"testing"

	"retrom/internal/cleanup"
	"retrom/internal/launch"
)

func TestLaunchBundleBytesAreCanonicalAcrossInputOrder(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	firstMetadata, err := server.blobs.Put(bytes.NewReader([]byte("first BIOS")))
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := server.blobs.Put(bytes.NewReader([]byte("second BIOS")))
	if err != nil {
		t.Fatal(err)
	}
	files := []launch.BundleFile{
		{LogicalName: "z-bios.bin", SHA256: secondMetadata.SHA256},
		{LogicalName: "a-bios.bin", SHA256: firstMetadata.SHA256},
	}
	readBundle := func(input []launch.BundleFile) []byte {
		t.Helper()
		bundle, bundleErr := server.createLaunchBundle(input)
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
}
