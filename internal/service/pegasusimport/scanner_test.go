package pegasusimport

import (
	"context"
	"errors"
	model "retrom/internal/model/pegasusimport"
	"strings"
	"testing"

	"retrom/internal/capability/format/pegasusmeta"
)

type scanSourceMemory struct {
	files    []DiscoveredFile
	contents map[string][]byte
	reads    []string
	readErr  error
}

func (source *scanSourceMemory) Discover(ctx context.Context, visit func(DiscoveredFile) error) error {
	for _, file := range source.files {
		if err := visit(file); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (source *scanSourceMemory) Metadata(_ context.Context, file DiscoveredFile) ([]byte, error) {
	source.reads = append(source.reads, file.Path)
	return source.contents[file.Path], source.readErr
}

func (*scanSourceMemory) Asset(context.Context, DiscoveredFile, string) (ScanAssetInspection, error) {
	return ScanAssetInspection{}, model.ErrSourceChanged
}

func scannerSourceFixture() *scanSourceMemory {
	body := []byte("collection: Test\ngame: Game\nfile: game.nes\n")
	return &scanSourceMemory{
		files: []DiscoveredFile{
			{Path: "metadata.pegasus.txt", Name: "metadata.pegasus.txt", Size: int64(len(body)), Facts: strings.Repeat("a", 64)},
			{Path: "game.nes", Name: "game.nes", Size: 1, Facts: strings.Repeat("b", 64)},
		},
		contents: map[string][]byte{"metadata.pegasus.txt": body},
	}
}

func TestScannerRejectsOversizedMetadataWithoutReadingIt(t *testing.T) {
	t.Parallel()
	source := scannerSourceFixture()
	source.files = append(source.files, DiscoveredFile{Path: "large/metadata.pegasus.txt", Name: "metadata.pegasus.txt", Size: pegasusmeta.MaxMetadataBytes + 1, Facts: strings.Repeat("c", 64)})
	result, err := NewScanner(source).Scan(t.Context())
	if err != nil || len(result.Items) != 1 || result.InvalidMetadata != 1 {
		t.Fatalf("independent evidence: %#v %v", result, err)
	}
	if len(source.reads) != 1 || source.reads[0] != "metadata.pegasus.txt" {
		t.Fatal("oversized metadata was read")
	}
	if result.Metadata[0].State != "INVALID" || result.Metadata[0].Digest != "" {
		t.Fatal("oversized evidence fabricated content hash")
	}
}

func TestScannerPreservesSourceAndIdentityCauses(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"source", "identity"} {
		t.Run(failure, func(t *testing.T) {
			source := scannerSourceFixture()
			service := NewScanner(source)
			cause := errors.New("scan boundary failed")
			if failure == "source" {
				source.readErr = cause
			} else {
				service.newID = func() (string, error) { return "", cause }
			}
			result, err := service.Scan(t.Context())
			if !errors.Is(err, cause) || len(result.Items) != 0 {
				t.Fatalf("failure lost or partial result: %v", err)
			}
		})
	}
}

func TestScannerSourceProjectionRetainsStableIdentityAndBlockedReasons(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body, code string
		collision        bool
	}{
		{name: "ready", body: "collection: Test\ngame: Game\nfile: game.nes\n"},
		{name: "case collision", body: "collection: Test\ngame: Game\nfile: game.nes\n", code: "PEGASUS_PATH_INVALID", collision: true},
		{name: "unsafe path", body: "collection: Test\ngame: Game\nfile: ../game.nes\n", code: "PEGASUS_PATH_INVALID"},
		{name: "orphan", body: "game: Game\nfile: game.nes\n", code: "PEGASUS_GAME_WITHOUT_COLLECTION"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := scannerSourceFixture()
			source.contents["metadata.pegasus.txt"] = []byte(test.body)
			source.files[0].Size = int64(len(test.body))
			if test.collision {
				source.files = append(source.files, DiscoveredFile{Path: "GAME.NES", Name: "GAME.NES", Size: 1, Facts: strings.Repeat("c", 64)})
			}
			service := NewScanner(source)
			first, err := service.Scan(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			second, err := service.Scan(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(first.Items) != 1 || len(second.Items) != 1 {
				t.Fatal("missing projected item")
			}
			if first.Items[0].DiscoveryCode != test.code {
				t.Fatalf("discovery code=%s", first.Items[0].DiscoveryCode)
			}
			if first.Items[0].SourceKey != second.Items[0].SourceKey || first.SnapshotDigest != second.SnapshotDigest {
				t.Fatal("identical source identity drifted")
			}
		})
	}
}
