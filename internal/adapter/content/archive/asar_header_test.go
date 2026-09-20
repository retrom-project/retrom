package archive

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"retrom/internal/capability/format/importing"
)

func TestASARHeaderRejectsOversizedDeclarationBeforeAllocatingOrReading(t *testing.T) {
	t.Parallel()
	var prefix [16]byte
	binary.LittleEndian.PutUint32(prefix[0:4], 4)
	binary.LittleEndian.PutUint32(prefix[4:8], (64<<20)+4)
	binary.LittleEndian.PutUint32(prefix[8:12], 64<<20)
	binary.LittleEndian.PutUint32(prefix[12:16], (64<<20)-4)
	reader := &countingHeaderReader{reader: bytes.NewReader(prefix[:])}
	members, offset, err := readASARHeader(reader, 1<<30, importing.DefaultArchiveLimits())
	if members != nil || offset != 0 || !errors.Is(err, importing.ErrElectronASARInvalid) ||
		err.Error() != "ARCHIVE_UNSAFE: ELECTRON_ASAR_INVALID: invalid pickle lengths" || reader.bytes != 16 || reader.calls != 1 {
		t.Fatalf("oversized header=%v offset=%d err=%v bytes=%d calls=%d", members, offset, err, reader.bytes, reader.calls)
	}
}

func TestASARHeaderPreservesTruncationAndPaddingFailureStages(t *testing.T) {
	t.Parallel()
	header := map[string]any{"files": map[string]any{"a": map[string]any{"size": 1, "offset": "0"}}}
	encoded := encodeASAR(t, header, []byte("a"))
	paddingStart := 16 + int(binary.LittleEndian.Uint32(encoded[12:16]))
	dataOffset := 8 + int(binary.LittleEndian.Uint32(encoded[4:8]))
	if dataOffset == paddingStart {
		t.Fatal("fixture needs actual pickle padding")
	}
	corrupt := append([]byte(nil), encoded...)
	corrupt[paddingStart] = 1
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{"prefix", encoded[:15], "truncated pickle header"},
		{"json", encoded[:17], "truncated JSON header"},
		{"padding", corrupt, "invalid pickle padding"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := readASARHeader(bytes.NewReader(test.body), int64(len(encoded)), importing.DefaultArchiveLimits())
			if err == nil || err.Error() != "ARCHIVE_UNSAFE: ELECTRON_ASAR_INVALID: "+test.want ||
				errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("failure stage/cause=%v", err)
			}
		})
	}
}

type countingHeaderReader struct {
	reader       io.Reader
	bytes, calls int
}

func (reader *countingHeaderReader) Read(buffer []byte) (int, error) {
	reader.calls++
	count, err := reader.reader.Read(buffer)
	reader.bytes += count
	return count, err
}
