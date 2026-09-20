package libraryimport

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	archiveadapter "retrom/internal/adapter/content/archive"
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/testkit/testsupport"
)

func TestProjectArchiveStageRejectsCorruptZIPDescriptor(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "body", body: []byte("terminal CRC is still pending")},
		{name: "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkProjectCorruptZIPDescriptor(t, test.body)
		})
	}
}

func checkProjectCorruptZIPDescriptor(t *testing.T, body []byte) {
	t.Helper()
	path := writeProjectCorruptZIPDescriptor(t, body)
	root := t.TempDir()
	store, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &testsupport.DiagnosticRecorder{}
	archives := archiveadapter.New(reporter)
	service := NewImportPreparation(nil, nil, store, ImportPreparationOptions{
		ProjectArchives: archives, ArchiveInspector: archives, Diagnostics: reporter,
	})
	entries, candidates, err := service.scanProjectArchivePath(t.Context(), path, contentprofile.ArchiveZIP)
	if entries != nil || candidates != nil || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("corrupt ZIP entries=%v candidates=%v error=%v", entries, candidates, err)
	}
	if paths := projectArchiveTemporaryPaths(t, filepath.Join(root, "tmp", "jobs")); len(paths) != 0 {
		t.Fatalf("failed Stage retained candidates: %v", paths)
	}
	published, err := os.ReadDir(filepath.Join(root, "blobs", "sha256"))
	if err != nil || len(published) != 0 {
		t.Fatalf("failed Stage published content: entries=%v err=%v", published, err)
	}
}

func writeProjectCorruptZIPDescriptor(t *testing.T, body []byte) string {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	member, err := writer.CreateHeader(&zip.FileHeader{Name: "data.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw := data.Bytes()
	offset := bytes.Index(raw, []byte{0x50, 0x4b, 0x07, 0x08})
	if offset < 0 || offset+16 > len(raw) {
		t.Fatal("fixture lacks a data descriptor")
	}
	crc := binary.LittleEndian.Uint32(raw[offset+4 : offset+8])
	binary.LittleEndian.PutUint32(raw[offset+4:offset+8], crc^1)
	path := filepath.Join(t.TempDir(), "descriptor.zip")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
