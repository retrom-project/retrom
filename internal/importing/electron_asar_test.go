package importing

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestScanElectronASARZIPStreamsPackedAndUnpackedMembers(t *testing.T) {
	t.Parallel()
	packed := map[string][]byte{
		"index.html":                    []byte("<!doctype html>"),
		"data/scenario/first.ks":        []byte("*start\n[cm]"),
		"data/scenario/translation.csv": {},
		"tyrano/tyrano.js":              []byte("window.tyrano = {};"),
	}
	unpacked := map[string][]byte{"data/native/helper.node": []byte("native-sidecar")}
	archivePath := writeElectronASARZIP(t, packed, unpacked, true)
	detected, err := DetectElectronASARZIP(archivePath, DefaultArchiveLimits())
	if err != nil || !detected {
		t.Fatalf("DetectElectronASARZIP() = %t, error=%v", detected, err)
	}
	consumed := make(map[string][]byte)
	entries, err := ScanElectronASARZIPWithConsumer(
		context.Background(), archivePath, DefaultArchiveLimits(),
		func(entry ArchiveEntry, reader io.Reader) (ArchiveContent, error) {
			contents, readErr := io.ReadAll(reader)
			if readErr != nil {
				return ArchiveContent{}, readErr
			}
			consumed[entry.NormalizedPath] = contents
			return testArchiveContent(contents), nil
		},
	)
	if err != nil || len(entries) != len(packed)+len(unpacked) {
		t.Fatalf("ScanElectronASARZIPWithConsumer() entries=%#v error=%v", entries, err)
	}
	for path, expected := range packed {
		if !bytes.Equal(consumed[path], expected) {
			t.Fatalf("packed %q = %q, want %q", path, consumed[path], expected)
		}
	}
	for path, expected := range unpacked {
		if !bytes.Equal(consumed[path], expected) {
			t.Fatalf("unpacked %q = %q, want %q", path, consumed[path], expected)
		}
	}
	for ordinal, entry := range entries {
		if entry.Ordinal != ordinal || entry.ArchiveFormat != "ELECTRON_ASAR" ||
			!strings.HasPrefix(entry.CompressionProfile, "ELECTRON_ASAR_") || entry.SHA256 == "" {
			t.Fatalf("entry[%d] = %#v", ordinal, entry)
		}
	}
}

func TestElectronASARZIPRejectsUnsafeHeadersAndIntegrityMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header map[string]any
		body   []byte
	}{
		{
			name: "path traversal",
			header: map[string]any{"files": map[string]any{
				"..": map[string]any{"files": map[string]any{"escape": asarTestFile(0, []byte("x"))}},
			}},
			body: []byte("x"),
		},
		{
			name: "out of bounds 64-bit offset",
			header: map[string]any{"files": map[string]any{
				"index.html": map[string]any{"size": 1, "offset": "4294967296"},
			}},
			body: []byte("x"),
		},
		{
			name: "integrity mismatch",
			header: map[string]any{"files": map[string]any{
				"index.html": map[string]any{
					"size": 1, "offset": "0",
					"integrity": map[string]any{
						"algorithm": "SHA256", "hash": strings.Repeat("0", 64),
						"blockSize": 1, "blocks": []string{strings.Repeat("0", 64)},
					},
				},
			}},
			body: []byte("x"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			archivePath := writeRawElectronASARZIP(t, encodeASAR(t, test.header, test.body), true, nil)
			_, err := ScanElectronASARZIPWithConsumer(
				context.Background(), archivePath, DefaultArchiveLimits(),
				func(_ ArchiveEntry, reader io.Reader) (ArchiveContent, error) {
					contents, readErr := io.ReadAll(reader)
					return testArchiveContent(contents), readErr
				},
			)
			if err == nil {
				t.Fatal("unsafe Electron ASAR was accepted")
			}
		})
	}
}

func TestDetectElectronASARZIPRequiresWindowsShellSibling(t *testing.T) {
	t.Parallel()
	archivePath := writeElectronASARZIP(t, map[string][]byte{"index.html": []byte("ok")}, nil, false)
	detected, err := DetectElectronASARZIP(archivePath, DefaultArchiveLimits())
	if err != nil || detected {
		t.Fatalf("DetectElectronASARZIP() = %t, error=%v", detected, err)
	}
}

func writeElectronASARZIP(
	t *testing.T,
	packed map[string][]byte,
	unpacked map[string][]byte,
	includeExecutable bool,
) string {
	t.Helper()
	header := map[string]any{"files": map[string]any{}}
	paths := make([]string, 0, len(packed)+len(unpacked))
	for name := range packed {
		paths = append(paths, name)
	}
	for name := range unpacked {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	var body bytes.Buffer
	for _, name := range paths {
		contents, isPacked := packed[name]
		if isPacked {
			insertASARTestNode(t, header, name, asarTestFile(body.Len(), contents))
			_, _ = body.Write(contents)
			continue
		}
		insertASARTestNode(t, header, name, map[string]any{
			"size": len(unpacked[name]), "unpacked": true,
		})
	}
	return writeRawElectronASARZIP(t, encodeASAR(t, header, body.Bytes()), includeExecutable, unpacked)
}

func asarTestFile(offset int, contents []byte) map[string]any {
	digest := sha256.Sum256(contents)
	return map[string]any{
		"size": len(contents), "offset": fmt.Sprintf("%d", offset),
		"integrity": map[string]any{
			"algorithm": "SHA256", "hash": hex.EncodeToString(digest[:]),
			"blockSize": max(1, len(contents)), "blocks": []string{hex.EncodeToString(digest[:])},
		},
	}
}

func insertASARTestNode(t *testing.T, root map[string]any, name string, value map[string]any) {
	t.Helper()
	node := root
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		files, ok := node["files"].(map[string]any)
		if !ok {
			t.Fatal("invalid ASAR test directory")
		}
		child, exists := files[part]
		if !exists {
			child = map[string]any{"files": map[string]any{}}
			files[part] = child
		}
		node, ok = child.(map[string]any)
		if !ok {
			t.Fatal("ASAR test path conflicts with a file")
		}
	}
	files, ok := node["files"].(map[string]any)
	if !ok {
		t.Fatal("invalid ASAR test leaf directory")
	}
	files[parts[len(parts)-1]] = value
}

func encodeASAR(t *testing.T, header map[string]any, body []byte) []byte {
	t.Helper()
	contents, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	payloadSize := 4 + len(contents)
	payloadSize += (4 - payloadSize%4) % 4
	result := make([]byte, 8+4+payloadSize+len(body))
	binary.LittleEndian.PutUint32(result[0:4], 4)
	binary.LittleEndian.PutUint32(result[4:8], uint32(4+payloadSize))
	binary.LittleEndian.PutUint32(result[8:12], uint32(payloadSize))
	binary.LittleEndian.PutUint32(result[12:16], uint32(len(contents)))
	copy(result[16:], contents)
	copy(result[8+4+payloadSize:], body)
	return result
}

func writeRawElectronASARZIP(
	t *testing.T,
	appASAR []byte,
	includeExecutable bool,
	unpacked map[string][]byte,
) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "electron.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entries := map[string][]byte{"Game/resources/app.asar": appASAR}
	if includeExecutable {
		entries["Game/Game.exe"] = []byte("MZ synthetic Electron shell")
	}
	for name, contents := range unpacked {
		entries["Game/resources/app.asar.unpacked/"+name] = contents
	}
	for name, contents := range entries {
		writer, createErr := archive.Create(name)
		if createErr == nil {
			_, createErr = writer.Write(contents)
		}
		if createErr != nil {
			t.Fatal(createErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func testArchiveContent(contents []byte) ArchiveContent {
	md5Digest := md5.Sum(contents)
	sha1Digest := sha1.Sum(contents)
	sha256Digest := sha256.Sum256(contents)
	return ArchiveContent{
		Size: int64(len(contents)), CRC32: fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)),
		MD5: hex.EncodeToString(md5Digest[:]), SHA1: hex.EncodeToString(sha1Digest[:]),
		SHA256: hex.EncodeToString(sha256Digest[:]),
	}
}
