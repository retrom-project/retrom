package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	datsource "retrom/internal/adapter/format/arcadedat"
	runtime "retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/arcadedat"
)

// Captured by the original loadCatalog at 2e98c03b59dc2a636092a523fd4d045d67ad2133.
// See testdata/catalog-lifecycle/provenance.json; never regenerate to accept a refactor difference.
const catalogOldGoGoldenSHA256 = "d3d5fd1ac961bfa41b5eba50a2f1eb01dcfbc48ccb05e13d875294b429e0d3a6"

func TestCatalogLoadReplaysOriginalGoObservations(t *testing.T) {
	expected := readCatalogGolden(t)
	cases := catalogReplayCases()
	if len(expected) != len(cases) {
		t.Fatalf("old observation count = %d, cases = %d", len(expected), len(cases))
	}
	for index, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			actual := observeCurrentCatalog(t, testCase)
			if !reflect.DeepEqual(actual, expected[index]) {
				t.Fatalf("old Go observation changed\nactual: %s\nwant:   %s",
					encodeCatalogObservation(t, actual), encodeCatalogObservation(t, expected[index]))
			}
		})
	}
}

func readCatalogGolden(t *testing.T) []catalogObservation {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", "catalog-lifecycle", "old-go-observations.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != catalogOldGoGoldenSHA256 {
		t.Fatal("original loadCatalog observation bytes changed; investigate without regenerating")
	}
	var observations []catalogObservation
	if err := json.Unmarshal(contents, &observations); err != nil {
		t.Fatal(err)
	}
	return observations
}

func observeCurrentCatalog(t *testing.T, testCase catalogReplayCase) catalogObservation {
	t.Helper()
	root := t.TempDir()
	if !testCase.Missing {
		if err := os.WriteFile(filepath.Join(root, "fixture.dat"), []byte(testCase.Contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if testCase.Cancel {
		cancel()
	}
	var events []string
	repository := &catalogRepositoryProbe{events: &events}
	if testCase.WriteFailure {
		repository.transactionErr = errCatalogFailureRecord
	}
	core := testCase.Core
	if core == "" {
		core = "fbneo"
	}
	reporter := &catalogDiagnosticProbe{events: new([]string)}
	bootstrap := catalogBootstrap{
		ctx: ctx, repository: repository, now: time.UnixMilli(1000), source: datsource.Source{}, reporter: reporter,
	}
	catalog, err := bootstrap.loadCatalog(
		&runtime.Version{DATRoot: root}, core, "fixture.dat", testCase.Expected, testCase.Indexed, "dat", "job",
	)
	if len(reporter.reports) != 0 {
		t.Fatalf("ordinary file Close unexpectedly failed: %#v", reporter.reports)
	}
	return catalogObservation{
		Name: testCase.Name, Catalog: catalog, Error: describeCatalogError(err, root),
		Events: events, Code: repository.code,
		Canceled: errors.Is(err, context.Canceled), Missing: errors.Is(err, os.ErrNotExist),
		WriteCause: errors.Is(err, errCatalogFailureRecord),
	}
}

func encodeCatalogObservation(t *testing.T, value catalogObservation) string {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func TestCatalogSourceErrorRetainsTheOriginalJoinChildren(t *testing.T) {
	harness := newCatalogLoadHarness(t.Context())
	pathCause := &os.PathError{Op: "open", Path: "unavailable.dat", Err: os.ErrNotExist}
	openFailure := fmt.Errorf("open built-in DAT: %w", pathCause)
	harness.source.openErr = openFailure
	harness.repository.transactionErr = errCatalogFailureRecord
	catalog, err := harness.load(catalogExpectedStats(), 0)
	if !reflect.DeepEqual(catalog, arcadedat.Catalog{}) {
		t.Fatalf("open failure returned catalog data: %#v", catalog)
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) != 2 {
		t.Fatalf("open/write errors do not form the original join tree: %T %v", err, err)
	}
	children := joined.Unwrap()
	if !errors.Is(children[0], openFailure) ||
		!reflect.DeepEqual(describeCatalogError(children[0], ""), describeCatalogError(openFailure, "")) {
		t.Fatalf("source error was replaced or wrapped again: %T %v", children[0], children[0])
	}
	if !errors.Is(children[1], errCatalogFailureRecord) || !errors.Is(err, pathCause) {
		t.Fatalf("join lost concrete open or write cause: %v", err)
	}
	assertCatalogEvents(t, harness, "open", "transaction")
	if harness.stream.reads != 0 || harness.stream.closes != 0 || len(harness.reporter.reports) != 0 {
		t.Fatal("failed acquisition transferred an unreadable resource or reported a Close")
	}
}

func TestCatalogErrorOraclePreservesNestedWrapperAndJoinNodes(t *testing.T) {
	first := fmt.Errorf("inner: %w", catalogOracleAsPanic{})
	joined := errors.Join(first, errors.New("second"))
	outer := fmt.Errorf("outer: %w", joined)
	actual := describeCatalogError(outer, "")
	want := &catalogErrorNode{
		Type: "*fmt.wrapError", Text: "outer: inner: first\nsecond",
		Children: []catalogErrorNode{{
			Type: "*errors.joinError", Text: "inner: first\nsecond",
			Children: []catalogErrorNode{
				{
					Type: "*fmt.wrapError", Text: "inner: first",
					Children: []catalogErrorNode{{Type: "dependencies.catalogOracleAsPanic", Text: "first"}},
				},
				{Type: "*errors.errorString", Text: "second"},
			},
		}},
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("error-tree oracle flattened or reordered a node\nactual: %#v\nwant: %#v", actual, want)
	}
}

type catalogOracleAsPanic struct{}

func (catalogOracleAsPanic) Error() string { return "first" }

func (catalogOracleAsPanic) As(any) bool {
	panic("error-tree oracle must not call custom As")
}
