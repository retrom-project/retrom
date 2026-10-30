//go:build integration

package scans

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestNormalizedPegasusImportRetainsMountedSourceAndRemovesInternalCopies(t *testing.T) {
	s, f, source, originals := normalizedSourceFixture(t, false)
	collections, err := s.inspect(t.Context(), model.SourceInput{Path: source, Format: "pegasus"})
	if err != nil || len(collections) != 1 || len(collections[0].Games) != 1 {
		t.Fatalf("source collection=%+v err=%v", collections, err)
	}
	root, err := s.Sources.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	scan := normalizationScan(t, f)
	collection := collections[0]
	if err = s.importGame(t.Context(), root, f.Principal, collection.Directory, collection.Games[0],
		model.SourceMapping{DirectoryID: f.DirectoryID}, &scan, nil); err != nil {
		t.Fatal(err)
	}
	if scan.ImportedCount != 1 {
		t.Fatalf("scan=%+v", scan)
	}
	assertNormalizedGamePackage(t, f)
	assertSourceUnchanged(t, source, originals)
	assertNoStaging(t, s)
}

func assertNormalizedGamePackage(t *testing.T, f testsupport.Fixture) {
	t.Helper()
	games, err := f.Repository.Games(t.Context(), f.Principal.User.ID, "pending_review", model.Query{Limit: 10})
	if err != nil || len(games.Items) != 1 {
		t.Fatalf("imported games=%+v err=%v", games, err)
	}
	detail, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, games.Items[0].ID, "pending_review")
	if err != nil || len(detail.Files) != 1 || detail.Files[0].Role != "project_package" {
		t.Fatalf("managed package=%+v err=%v", detail.Files, err)
	}
	if detail.Files[0].ExtInfo.ConversionInfo != nil {
		t.Fatal("non-KRKR conversion unexpectedly retained an original")
	}
	logical, err := f.Repository.ContentFiles(t.Context(), detail.Files)
	if err != nil || len(logical) != 2 {
		t.Fatalf("logical files=%+v err=%v", logical, err)
	}
	for _, file := range logical {
		if file.LogicalKey == "fixture.exe" {
			t.Fatal("original imported container survived in normalized package")
		}
	}
}

func TestRejectedNormalizationAndCancellationRetainMountedSource(t *testing.T) {
	s, f, source, originals := normalizedSourceFixture(t, true)
	root, err := s.Sources.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	scan := normalizationScan(t, f)
	candidate := Candidate{Input: f.Game.Input, Files: []string{"fixture.exe"}}
	if err = s.importGame(t.Context(), root, f.Principal, ".", candidate,
		model.SourceMapping{DirectoryID: f.DirectoryID}, &scan, nil); err == nil {
		t.Fatal("invalid project container was imported")
	}
	operation := uuid.NewString()
	input, err := s.copy(t.Context(), operation, "fixture.exe", bytes.NewReader(originals["fixture.exe"]))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.normalize(ctx, operation, "tyranoscript", []model.ContentFile{input})
	s.removeStaging(operation)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("normalization cancellation=%v", err)
	}
	if count, readErr := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_tab"); readErr != nil || count != 0 {
		t.Fatalf("failed import left game count=%d err=%v", count, readErr)
	}
	assertSourceUnchanged(t, source, originals)
	assertNoStaging(t, s)
}

func TestMatchedConverterFailureReportsStageAndNeverPublishesSourceFallback(t *testing.T) {
	s, f, source, originals := normalizedSourceFixture(t, false)
	// A recognizable embedded project with a bad member checksum matches the
	// converter, then fails verification. It must not fall back to the executable.
	body := originals["fixture.exe"]
	at := bytes.LastIndex(body, []byte{'P', 'K', 1, 2})
	if at < 0 {
		t.Fatal("fixture central directory absent")
	}
	body[at+16] ^= 1
	if err := os.WriteFile(filepath.Join(source, "fixture.exe"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := s.Sources.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	scan := normalizationScan(t, f)
	err = s.importGame(t.Context(), root, f.Principal, ".", Candidate{Input: f.Game.Input, Files: []string{"fixture.exe"}},
		model.SourceMapping{DirectoryID: f.DirectoryID}, &scan, nil)
	var contentError *model.RuntimeContentError
	if !errors.As(err, &contentError) || contentError.Code != "CONTENT_NORMALIZATION_FAILED" ||
		importFailureCode(err) != "CONTENT_NORMALIZATION_FAILED" {
		t.Fatalf("matched conversion failure lost: %v", err)
	}
	if count, readErr := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_tab"); readErr != nil || count != 0 {
		t.Fatalf("matched failure published fallback: count=%d err=%v", count, readErr)
	}
	assertSourceUnchanged(t, source, originals)
	assertNoStaging(t, s)
}

func normalizedSourceFixture(t *testing.T, invalid bool) (*Service, testsupport.Fixture, string, map[string][]byte) {
	t.Helper()
	f := testsupport.Library(t)
	store, runtime := mameScanRuntime(t)
	core := ""
	for _, binding := range runtime.Bindings {
		if slices.Contains(binding.PlatformIDs, "tyranoscript") {
			core = binding.CoreID
			break
		}
	}
	if core == "" {
		t.Fatal("declared runtime lacks native-web fixture core")
	}
	directory := f.Directory
	directory.Version, directory.PlatformID, directory.DefaultCoreID, directory.CoreIDs = 1, "tyranoscript", core, []string{core}
	if err := f.Repository.WriteDirectory(t.Context(), f.DirectoryID, directory, 2000, false); err != nil {
		t.Fatal(err)
	}
	s := &Service{Repository: f.Repository, Runtime: runtime, Storage: store, Now: time.Now}
	source := t.TempDir()
	originals := map[string][]byte{
		"metadata.pegasus.txt": []byte("collection: Project fixture\n\ngame: Native web fixture\nfile: fixture.exe\n"),
		"fixture.exe":          embeddedProjectFixture(t),
		"cover.png":            []byte("unselected mounted source must survive too"),
	}
	if invalid {
		originals["fixture.exe"] = []byte("MZ invalid embedded project")
	}
	for name, body := range originals {
		if err := os.WriteFile(filepath.Join(source, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return s, f, source, originals
}

func embeddedProjectFixture(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range map[string]string{"index.html": "<!doctype html><html>project fixture</html>", "data/scene.ks": "fixture dialog"} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return append([]byte("MZ inert fixture; never executable\n"), buffer.Bytes()...)
}

func normalizationScan(t *testing.T, f testsupport.Fixture) model.Scan {
	t.Helper()
	scan := model.Scan{ID: uuid.NewString(), ScanType: "game", Status: "running", TotalKnown: true, TotalCount: 1, CreatedAtMs: 1000}
	if err := f.Repository.CreateScan(t.Context(), scan, f.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	return scan
}

func assertSourceUnchanged(t *testing.T, root string, originals map[string][]byte) {
	t.Helper()
	for name, expected := range originals {
		actual, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("mounted source %s changed or removed: err=%v", name, err)
		}
	}
}

func assertNoStaging(t *testing.T, s *Service) {
	t.Helper()
	path, err := s.Storage.Absolute("temporary")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("original import copies or normalization outputs retained: %v", entries)
	}
}
