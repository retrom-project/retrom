package pegasusimport

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/capability/format/pegasusmeta"
	application "retrom/internal/model/pegasusimport"
)

func TestMetadataVerificationAllowsOnlyOversizedInvalidFactsWithoutContentDigest(t *testing.T) {
	t.Parallel()
	root := Root{path: t.TempDir()}
	name := "metadata.pegasus.txt"
	filename := filepath.Join(root.path, name)
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(pegasusmeta.MaxMetadataBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	expected := application.MetadataEvidence{RelativePath: name, SizeBytes: info.Size(), FactsDigest: serversource.FactsDigest(info), ParseState: "INVALID", ErrorCode: pegasusmeta.ErrTooLarge.Error()}
	source := (&Service{}).acquireSourceReader
	if err := verifyMetadataFile(t.Context(), root, "", expected, source); err != nil {
		t.Fatalf("facts-only oversized evidence rejected: %v", err)
	}
	expected.ParseState = "VALID"
	if err := verifyMetadataFile(t.Context(), root, "", expected, source); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("missing hash allowed valid metadata: %v", err)
	}
	expected.ParseState = "INVALID"
	expected.FactsDigest = "changed"
	if err := verifyMetadataFile(t.Context(), root, "", expected, source); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("changed oversized metadata accepted: %v", err)
	}
}
