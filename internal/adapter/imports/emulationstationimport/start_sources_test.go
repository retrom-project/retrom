package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/adapter/files/serversource"
	application "retrom/internal/model/emulationstationimport"
)

func startSourceEvidence(t *testing.T) (Root, application.GamelistEvidence) {
	t.Helper()
	root := Root{ID: "games", path: t.TempDir()}
	data := []byte("<gameList></gameList>")
	path := filepath.Join(root.path, "gamelist.xml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	encoded := hex.EncodeToString(digest[:])
	return root, application.GamelistEvidence{RelativePath: "gamelist.xml", FactsDigest: serversource.FactsDigest(info), ContentDigest: &encoded, ParseState: "VALID", SizeBytes: int64(len(data))}
}

func TestStartSourceReaderRejectsContentFactsAndPathDrift(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"unchanged", "digest", "facts", "missing", "symlink", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root, evidence := startSourceEvidence(t)
			ctx := t.Context()
			var cause error
			switch kind {
			case "digest":
				digest := strings.Repeat("a", 64)
				evidence.ContentDigest = &digest
			case "facts":
				evidence.FactsDigest = strings.Repeat("a", 64)
			case "missing":
				evidence.RelativePath = "missing.xml"
				cause = os.ErrNotExist
			case "symlink":
				if err := os.Symlink(filepath.Join(root.path, "gamelist.xml"), filepath.Join(root.path, "link.xml")); err != nil {
					t.Fatal(err)
				}
				evidence.RelativePath = "link.xml"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				cause = context.Canceled
			}
			err := verifyStartGamelist(ctx, root, "", evidence)
			if kind == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || kind != "cancelled" && !errors.Is(err, ErrSourceChanged) || cause != nil && !errors.Is(err, cause) {
				t.Fatalf("%s evidence error=%v", kind, err)
			}
		})
	}
}

func TestStartAcceptsScannedOversizedGamelistFactsWithoutHashing(t *testing.T) {
	fixture := newLifecycleFixture(t)
	path := filepath.Join(fixture.source, "oversized", "gamelist.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(application.MaxSnapshotGamelistBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	started, _ := startLifecycleImport(t, fixture, "", "nes")
	if started.State != "QUEUED" || started.Counts.InvalidGamelists != 1 {
		t.Fatalf("oversized invalid evidence prevented start: %#v", started)
	}
}

func TestStartRepeatedCurrentVersionDoesNotReopenSource(t *testing.T) {
	fixture, mapped, _ := queryMappedTagFixture(t)
	started, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.source); err != nil {
		t.Fatal(err)
	}
	repeated, err := fixture.service.StartImport(fixture.context, started.ID, started.Version)
	if err != nil || repeated.Version != started.Version || repeated.ImportJobID == nil || *repeated.ImportJobID != *started.ImportJobID {
		t.Fatalf("repeated start=%#v error=%v", repeated, err)
	}
	if _, err := fixture.service.StartImport(fixture.context, started.ID, mapped.Version); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale repeated start error=%v", err)
	}
}
