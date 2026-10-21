package scans

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func TestZIPUnicodeNamesSurviveImport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"怒.png", "\xc5\xad.png"},
		{"微笑.png", "\xce\xa2\xd0\xa6.png"},
		{"平原.png", "\xc6\xbd\xd4\xad.png"},
		{"学校.png", "\xd1\xa7\xd0\xa3.png"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if closeErr := store.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			})
			source := writeZIPNameFixture(t, zip.FileHeader{
				Name: test.raw, NonUTF8: true, Extra: unicodePathExtra(test.raw, []byte(test.name)),
			})
			root, err := os.OpenRoot(filepath.Dir(source))
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot(root)
			s := &Service{Storage: store}
			files, err := s.archive(t.Context(), root, "11111111-1111-4111-8111-111111111111", "source.zip", ".zip")
			if err != nil || len(files) != 1 || files[0].LogicalKey != test.name {
				t.Fatalf("files=%+v error=%v; expected Unicode name %q", files, err, test.name)
			}
			assertStoredBytes(t, store, files[0].StorageKey, []byte("image payload"))
		})
	}
}

func TestZIPNameMetadataPrecedence(t *testing.T) {
	t.Parallel()
	raw := "\xc5\xad.png"
	valid := unicodePathExtra(raw, []byte("怒.png"))
	badCRC := bytes.Clone(valid)
	badCRC[5] ^= 1
	unknownVersion := bytes.Clone(valid)
	unknownVersion[4] = 2
	cases := []struct {
		name   string
		header zip.FileHeader
		want   string
	}{
		{"Unicode metadata", zip.FileHeader{Name: raw, NonUTF8: true, Extra: valid}, "怒.png"},
		{"UTF8 flag", zip.FileHeader{Name: "雪.png", Flags: 0x800, Extra: unicodePathExtra("雪.png", []byte("other.png"))}, "雪.png"},
		{"checksum mismatch", zip.FileHeader{Name: raw, NonUTF8: true, Extra: badCRC}, "ŭ.png"},
		{"unknown version", zip.FileHeader{Name: raw, NonUTF8: true, Extra: unknownVersion}, "ŭ.png"},
		{"unmarked UTF8", zip.FileHeader{Name: "雪.png", NonUTF8: true}, "雪.png"},
		{"unmarked GB18030", zip.FileHeader{Name: "\xd3\xce\xcf\xb7.png", NonUTF8: true}, "游戏.png"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			entries, err := readZIPNameFixture(t, test.header)
			if err != nil || len(entries) != 1 || entries[0].name != test.want {
				t.Fatalf("entries=%+v error=%v; expected %q", entries, err, test.want)
			}
		})
	}
}

func TestZIPUnicodeNamesKeepContentValidation(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../escape.png", "/escape.png", "directory\\escape.png"} {
		t.Run(name, func(t *testing.T) {
			entries, err := readZIPNameFixture(t, zip.FileHeader{
				Name: "safe.png", Extra: unicodePathExtra("safe.png", []byte(name)),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = (&Service{}).extract(t.Context(), "unused", entries)
			if !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("unsafe Unicode path accepted: %q; error=%v", name, err)
			}
		})
	}
}

func TestZIPRejectsInvalidUnicodeNameMetadata(t *testing.T) {
	t.Parallel()
	for _, extra := range [][]byte{
		unicodePathExtra("safe.png", []byte{0xff}),
		unicodePathExtra("safe.png", nil),
		{0x75, 0x70, 0x08, 0x00, 1},
	} {
		if _, err := readZIPNameFixture(t, zip.FileHeader{Name: "safe.png", Extra: extra}); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid Unicode metadata accepted: %x; error=%v", extra, err)
		}
	}
	if _, err := readZIPNameFixture(t, zip.FileHeader{Name: "\xff.png", NonUTF8: true, Flags: 0x800}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("invalid explicitly UTF-8 name accepted: %v", err)
	}
}

func TestZIPRejectsDuplicateUnicodeNames(t *testing.T) {
	t.Parallel()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeErr := store.Close()
		if closeErr != nil {
			t.Error(closeErr)
		}
	})
	path := writeZIPNameFixture(t,
		zip.FileHeader{Name: "first.png", Extra: unicodePathExtra("first.png", []byte("same.png"))},
		zip.FileHeader{Name: "second.png", Extra: unicodePathExtra("second.png", []byte("same.png"))},
	)
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	s := &Service{Storage: store}
	_, err = s.archive(t.Context(), root, "11111111-1111-4111-8111-111111111111", "source.zip", ".zip")
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("duplicate decoded paths accepted: %v", err)
	}
}

func unicodePathExtra(raw string, name []byte) []byte {
	extra := make([]byte, 9+len(name))
	binary.LittleEndian.PutUint16(extra, 0x7075)
	binary.LittleEndian.PutUint16(extra[2:], uint16(5+len(name)))
	extra[4] = 1
	binary.LittleEndian.PutUint32(extra[5:], crc32.ChecksumIEEE([]byte(raw)))
	copy(extra[9:], name)
	return extra
}

func writeZIPNameFixture(t *testing.T, headers ...zip.FileHeader) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, header := range headers {
		member, err := writer.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = member.Write([]byte("image payload")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	// archive/zip clears an inconsistent UTF-8 flag; set it in both records to test malformed input.
	data := buffer.Bytes()
	if len(headers) == 1 && headers[0].Flags&0x800 != 0 {
		central := bytes.Index(data, []byte{'P', 'K', 1, 2})
		if central < 0 {
			t.Fatal("missing ZIP directory fixture")
		}
		for _, offset := range []int{6, central + 8} {
			flags := binary.LittleEndian.Uint16(data[offset:])
			binary.LittleEndian.PutUint16(data[offset:], flags|0x800)
		}
	}
	path := filepath.Join(t.TempDir(), "source.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readZIPNameFixture(t *testing.T, header zip.FileHeader) ([]entry, error) {
	t.Helper()
	file, err := os.Open(writeZIPNameFixture(t, header))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFile(file) })
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return zipEntries(file, info.Size())
}
