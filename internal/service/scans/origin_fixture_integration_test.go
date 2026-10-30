//go:build integration

package scans

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/adler32"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"

	"retrom/internal/testsupport"
)

func krkrOriginFixture(t *testing.T) (*Service, testsupport.Fixture, string, map[string][]byte) {
	t.Helper()
	f := testsupport.Library(t)
	store, client := mameScanRuntime(t)
	directory := f.Directory
	directory.Version, directory.PlatformID = 1, "kirikiri"
	directory.DefaultCoreID, directory.CoreIDs = "kirikiri2", []string{"kirikiri2"}
	if err := f.Repository.WriteDirectory(t.Context(), f.DirectoryID, directory, 2000, false); err != nil {
		t.Fatal(err)
	}
	s := &Service{Repository: f.Repository, Runtime: client, Storage: store, Now: time.Now}
	root := t.TempDir()
	originals := map[string][]byte{
		"metadata.pegasus.txt": []byte("collection: KRKR origin fixture\n\ngame: Synthetic protected XP3\nfile: data.xp3\n"),
		"data.xp3":             originXP3(true),
		"cover.png":            []byte("unselected source must not be archived"),
	}
	for name, body := range originals {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return s, f, root, originals
}

// An ordinary XP3 with three independent checksums and a fixed XOR filter.
// All resources are owned, inert test text; no original game/module is used.
func originXP3(protected bool) []byte {
	var data, index bytes.Buffer
	for i, name := range []string{"startup.tjs", "first.ks", "readme.txt"} {
		plain := []byte("// Retrom origin regression " + name + "\n" + string(bytes.Repeat([]byte{byte('a' + i)}, 120)))
		body := bytes.Clone(plain)
		if protected {
			for at := range body {
				body[at] ^= 0x73
			}
		}
		encoded := utf16.Encode([]rune(name))
		info := make([]byte, 22+2*len(encoded))
		if protected {
			binary.LittleEndian.PutUint32(info, 0x80000000)
		}
		binary.LittleEndian.PutUint64(info[4:], uint64(len(body)))
		binary.LittleEndian.PutUint64(info[12:], uint64(len(body)))
		binary.LittleEndian.PutUint16(info[20:], uint16(len(encoded)))
		for at, char := range encoded {
			binary.LittleEndian.PutUint16(info[22+2*at:], char)
		}
		segment := make([]byte, 28)
		binary.LittleEndian.PutUint64(segment[4:], uint64(19+data.Len()))
		binary.LittleEndian.PutUint64(segment[12:], uint64(len(body)))
		binary.LittleEndian.PutUint64(segment[20:], uint64(len(body)))
		checksum := make([]byte, 4)
		binary.LittleEndian.PutUint32(checksum, adler32.Checksum(plain))
		row := append(originXP3Chunk("info", info), originXP3Chunk("segm", segment)...)
		row = append(row, originXP3Chunk("adlr", checksum)...)
		index.Write(originXP3Chunk("File", row))
		data.Write(body)
	}

	header := make([]byte, 19)
	copy(header, []byte{'X', 'P', '3', 13, 10, 32, 10, 26, 139, 103, 1})
	binary.LittleEndian.PutUint64(header[11:], uint64(19+data.Len()))
	var archive bytes.Buffer
	archive.Grow(len(header) + data.Len() + 9 + index.Len())
	archive.Write(header)
	archive.Write(data.Bytes())
	archive.WriteByte(0)
	size := make([]byte, 8)
	binary.LittleEndian.PutUint64(size, uint64(index.Len()))
	archive.Write(size)
	archive.Write(index.Bytes())
	return archive.Bytes()
}

func originXP3Chunk(name string, body []byte) []byte {
	chunk := make([]byte, 12+len(body))
	copy(chunk, name)
	binary.LittleEndian.PutUint64(chunk[4:], uint64(len(body)))
	copy(chunk[12:], body)
	return chunk
}

func originalXP3Zip(t *testing.T, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("data.xp3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
