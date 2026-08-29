package contentmanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"retrom/internal/testassert"
)

func TestBuildV2UsesCanonicalSummaryAndDigest(t *testing.T) {
	archive := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ordinal := 2
	files := []File{
		{
			Role:        "DOS_SOURCE",
			LogicalName: "Z.EXE",
			BlobSHA256:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			SizeBytes:   3,
		},
		{
			Role:                      "CONTENT",
			LogicalName:               "A.gba",
			BlobSHA256:                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SizeBytes:                 2,
			SourceArchiveSHA256:       &archive,
			SourceArchiveEntryOrdinal: &ordinal,
		},
	}
	contents, digest, err := Build("DOS_BUNDLE", files)
	filesDigest, totalBytes, digestErr := FilesDigest(files)
	want := `{"contentKind":"DOS_BUNDLE","fileCount":2,"filesDigest":"` + filesDigest + `","schemaVersion":2,"totalBytes":5}`
	wantDigest := sha256.Sum256([]byte(want))
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return digestErr != nil },
		func() bool { return totalBytes != 5 },
		func() bool { return string(contents) != want },
		func() bool { return digest != hex.EncodeToString(wantDigest[:]) },
	), "manifest = %s digest=%s filesDigest=%s error=%v/%v", contents, digest, filesDigest, err, digestErr)
}

func TestFilesDigestRejectsInvalidAndDuplicateEntries(t *testing.T) {
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
			if _, _, err := FilesDigest(test.files); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want %v", err, ErrInvalid)
			}
		})
	}
}

func TestBuildRejectsUnknownContentKind(t *testing.T) {
	if _, _, err := Build("UNKNOWN", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want %v", err, ErrInvalid)
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
