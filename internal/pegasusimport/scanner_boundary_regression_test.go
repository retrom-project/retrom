package pegasusimport

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/pegasusmeta"

	"github.com/google/uuid"
)

func TestScannerCancelledEmptyDirectoryKeepsContextCause(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (&Service{}).scan(ctx, Root{path: t.TempDir()}, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan returned %v", err)
	}
}

func scannerBoundarySource(t *testing.T) Root {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "metadata.pegasus.txt"), []byte("collection: Test\ngame: Game\nfile: game.nes\n"))
	writeFixture(t, filepath.Join(root, "game.nes"), []byte("retrom test source"))
	return Root{ID: "games", path: root, digest: strings.Repeat("a", 64)}
}

// Sequential: restore the shared UUID entropy source before any parallel test runs.
func TestScannerCollectionAndItemIDsPreserveEntropyFailure(t *testing.T) {
	for _, preceding := range []int{0, 16} {
		name := "collection"
		if preceding != 0 {
			name = "item"
		}
		t.Run(name, func(t *testing.T) {
			root := scannerBoundarySource(t)
			uuid.SetRand(io.MultiReader(strings.NewReader(strings.Repeat("x", preceding)), unavailableCreationEntropy{}))
			result, err := func() (scanResult, error) { defer uuid.SetRand(nil); return (&Service{}).scan(t.Context(), root, "") }()
			if !errors.Is(err, errCreationEntropy) || len(result.Items) != 0 {
				t.Fatalf("%s identity failure ignored: %v", name, err)
			}
		})
	}
}

func TestScannerOversizedMetadataKeepsIndependentEvidence(t *testing.T) {
	t.Parallel()
	service, unit, _ := scanPublicationFixture(t)
	root := scannerBoundarySource(t)
	extra := filepath.Join(root.path, "oversized")
	if err := os.Mkdir(extra, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(extra, "metadata.pegasus.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(pegasusmeta.MaxMetadataBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := service.scan(t.Context(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.InvalidMetadata != 1 || len(result.Items) != 1 {
		t.Fatal("oversized file blocked independent valid metadata")
	}
	if err := service.persistScan(t.Context(), unit, result); err != nil {
		t.Fatalf("oversized evidence could not be published: %v", err)
	}
}

func TestScannerMetadataUsesSharedReaderBudget(t *testing.T) {
	t.Parallel()
	root := scannerBoundarySource(t)
	cause := errors.New("reader budget cancelled")
	hits := 0
	service := &Service{sourceReader: func(context.Context) (func(), error) { hits++; return nil, cause }}
	_, err := service.scan(t.Context(), root, "")
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("metadata reader bypassed budget: hits=%d err=%v", hits, err)
	}
}
