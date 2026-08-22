package contentmanifest

import (
	"testing"

	"retrom/internal/testassert"
)

func TestBuildSortsFilesAndUsesCanonicalFields(t *testing.T) {
	archive := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ordinal := 2
	contents, digest, err := Build([]File{
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
	})
	want := `[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"A.gba","role":"CONTENT","sizeBytes":2,"sourceArchiveEntryOrdinal":2,"sourceArchiveSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},{"blobSha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","logicalName":"Z.EXE","role":"DOS_SOURCE","sizeBytes":3}]`
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return string(contents) != want }, func() bool { return len(digest) != 64 }), "manifest = %s digest=%s error=%v", contents, digest, err)
}

func TestBuildRejectsHalfArchiveReference(t *testing.T) {
	archive := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, _, err := Build([]File{{Role: "CONTENT", LogicalName: "A", BlobSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SourceArchiveSHA256: &archive}}); err == nil {
		t.Fatal("half archive reference accepted")
	}
}
