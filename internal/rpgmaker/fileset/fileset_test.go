package fileset

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"retrom/internal/testassert"
)

func TestDigestUsesExactBinaryEncodingAndOrder(t *testing.T) {
	files := []File{
		{Role: "PROJECT_FILE", LogicalName: "z", BlobSHA256: repeatHex("02"), SizeBytes: 4},
		{Role: "PROJECT_FILE", LogicalName: "a", BlobSHA256: repeatHex("01"), SizeBytes: 3},
	}
	got, total, err := Digest(files)
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(domain))
	for _, file := range []File{files[1], files[0]} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(file.Role)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(file.Role))
		binary.BigEndian.PutUint32(length[:], uint32(len(file.LogicalName)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(file.LogicalName))
		raw, _ := hex.DecodeString(file.BlobSHA256)
		_, _ = hasher.Write(raw)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(file.SizeBytes))
		_, _ = hasher.Write(size[:])
		_, _ = hasher.Write([]byte{0})
	}
	want := hex.EncodeToString(hasher.Sum(nil))
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return total != 7 },
		func() bool { return got != want },
	), "digest=%s want=%s total=%d err=%v", got, want, total, err)
}

func TestDigestRejectsMalformedOrDuplicateEntries(t *testing.T) {
	archive := repeatHex("bb")
	ordinal := 1
	valid := File{Role: "CONTENT", LogicalName: "a", BlobSHA256: repeatHex("aa"), SizeBytes: 1}
	tests := []struct {
		name  string
		files []File
	}{
		{name: "half archive", files: []File{{Role: "CONTENT", LogicalName: "a", BlobSHA256: repeatHex("aa"), SourceArchiveSHA256: &archive}}},
		{name: "uppercase digest", files: []File{{Role: "CONTENT", LogicalName: "a", BlobSHA256: repeatHex("AA")}}},
		{name: "invalid utf8", files: []File{{Role: "CONTENT", LogicalName: string([]byte{0xff}), BlobSHA256: repeatHex("aa")}}},
		{name: "negative ordinal", files: []File{{Role: "CONTENT", LogicalName: "a", BlobSHA256: repeatHex("aa"), SourceArchiveSHA256: &archive, SourceArchiveEntryOrdinal: intPointer(-1)}}},
		{name: "duplicate", files: []File{valid, valid}},
		{name: "invalid role", files: []File{{Role: "UNKNOWN", LogicalName: "a", BlobSHA256: repeatHex("aa")}}},
		{name: "bad archive digest", files: []File{{Role: "CONTENT", LogicalName: "a", BlobSHA256: repeatHex("aa"), SourceArchiveSHA256: stringPointer("bad"), SourceArchiveEntryOrdinal: &ordinal}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := Digest(test.files); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want %v", err, ErrInvalid)
			}
		})
	}
}

func repeatHex(pair string) string {
	value := ""
	for range sha256.Size {
		value += pair
	}
	return value
}

func intPointer(value int) *int          { return &value }
func stringPointer(value string) *string { return &value }
