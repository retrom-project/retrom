package dependencies

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	runtime "retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/arcadedat"
	model "retrom/internal/model/dependencies"
	"retrom/internal/model/diagnostics"
)

func TestCatalogLoadClosesSourceBeforeReturningValidValues(t *testing.T) {
	harness := newCatalogLoadHarness(t.Context())
	catalog, err := harness.load(catalogExpectedStats(), 0)
	if err != nil || len(catalog.Machines) != 1 || catalog.Machines[0].Name != "demo" {
		t.Fatalf("load catalog = %#v, %v", catalog, err)
	}
	assertCatalogEvents(t, harness, "open", "read", "close")
	if harness.stream.closes != 1 || harness.source.root != "frozen-dat-root" ||
		harness.source.relative != "fbneo/fixture.dat" || len(harness.reporter.reports) != 0 {
		t.Fatalf("resource ownership: closes=%d root=%q relative=%q diagnostics=%v",
			harness.stream.closes, harness.source.root, harness.source.relative, harness.reporter.reports)
	}
}

func TestCatalogIndexedShortcutDoesNotAcquireAResource(t *testing.T) {
	harness := newCatalogLoadHarness(t.Context())
	harness.source.openErr = errors.New("unexpected source access")
	catalog, err := harness.load(catalogExpectedStats(), 1)
	want := arcadedat.Catalog{Stats: arcadedat.Stats{MachineCount: 1, ROMEntryCount: 1}}
	if err != nil || !reflect.DeepEqual(catalog, want) || len(*harness.events) != 0 {
		t.Fatalf("indexed shortcut: catalog=%#v events=%v err=%v", catalog, *harness.events, err)
	}
}

func TestCatalogCloseFailuresAreNonfatalSanitizedDiagnostics(t *testing.T) {
	var typedNil *catalogPoisonCloseError
	tests := []struct {
		name      string
		cause     error
		errorType string
	}{
		{"path", &os.PathError{Op: "close", Path: "/private/secret-token", Err: errors.New("private-token")}, "*fs.PathError"},
		{"plain", errors.New("private-token"), "*errors.errorString"},
		{"poison", &catalogPoisonCloseError{}, "*dependencies.catalogPoisonCloseError"},
		{"typed-nil", typedNil, "*dependencies.catalogPoisonCloseError"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newCatalogLoadHarness(t.Context())
			harness.stream.closeErr = testCase.cause
			catalog, err := harness.load(catalogExpectedStats(), 0)
			if err != nil || catalog.Stats.MachineCount != 1 {
				t.Fatalf("nonfatal Close changed successful catalog: %#v, %v", catalog, err)
			}
			assertCatalogEvents(t, harness, "open", "read", "close", "report")
			assertCatalogDiagnostic(t, harness, testCase.errorType)
		})
	}
}

func TestCatalogCancellationStillReadsBeforeParsingAndCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	harness := newCatalogLoadHarness(ctx)
	catalog, err := harness.load(catalogExpectedStats(), 0)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(catalog, arcadedat.Catalog{}) {
		t.Fatalf("canceled parse result: %#v, %v", catalog, err)
	}
	if harness.stream.reads == 0 || len(harness.stream.contents) != 0 || harness.stream.closes != 1 {
		t.Fatalf("cancellation changed initial read/Close: reads=%d remaining=%d closes=%d",
			harness.stream.reads, len(harness.stream.contents), harness.stream.closes)
	}
	assertCatalogEvents(t, harness, "open", "read", "close", "transaction", "mark-failed", "finish")
	if harness.repository.code != "DEPENDENCY_DAT_PARSE_FAILED" {
		t.Fatalf("canceled parser classification = %q", harness.repository.code)
	}
}

func TestCatalogReadFailureWinsCancellationAndRetainsWriteCause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	harness := newCatalogLoadHarness(ctx)
	readFailure := errors.New("read source failed")
	harness.stream.contents = []byte("<unfinished")
	harness.stream.readErr = readFailure
	harness.stream.closeErr = &catalogPoisonCloseError{}
	harness.repository.transactionErr = errCatalogFailureRecord
	_, err := harness.load(catalogExpectedStats(), 0)
	if !errors.Is(err, readFailure) || !errors.Is(err, errCatalogFailureRecord) ||
		errors.Is(err, context.Canceled) || errors.Is(err, harness.stream.closeErr) {
		t.Fatalf("read failure precedence or causes changed: %v", err)
	}
	assertCatalogEvents(t, harness, "open", "read", "close", "report", "transaction")
	assertCatalogDiagnostic(t, harness, "*dependencies.catalogPoisonCloseError")
	if harness.reporter.contexts[0] != ctx {
		t.Fatal("diagnostic lost the original canceled context")
	}
}

func TestCatalogContentFailuresCloseBeforeRecordingFailure(t *testing.T) {
	tests := []struct {
		name, contents, code string
		expected             model.CatalogStats
		cause                error
	}{
		{
			name: "parse", contents: "<datafile><game></datafile>", expected: catalogExpectedStats(),
			code: "DEPENDENCY_DAT_PARSE_FAILED", cause: arcadedat.ErrInvalidDocument,
		},
		{
			name: "statistics", contents: catalogFixtureXML, expected: model.CatalogStats{MachineCount: 2},
			code: "DEPENDENCY_DAT_STATISTICS_MISMATCH", cause: runtime.ErrInvalid,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newCatalogLoadHarness(t.Context())
			harness.stream.contents = []byte(testCase.contents)
			harness.stream.closeErr = &catalogPoisonCloseError{}
			catalog, err := harness.load(testCase.expected, 0)
			if !errors.Is(err, testCase.cause) || !reflect.DeepEqual(catalog, arcadedat.Catalog{}) {
				t.Fatalf("content failure lost primary or returned partial values: %#v %v", catalog, err)
			}
			assertCatalogEvents(t, harness, "open", "read", "close", "report", "transaction", "mark-failed", "finish")
			assertCatalogDiagnostic(t, harness, "*dependencies.catalogPoisonCloseError")
			if harness.repository.code != testCase.code {
				t.Fatalf("persisted error code = %q, want %q", harness.repository.code, testCase.code)
			}
		})
	}
}

func TestCatalogFailureRecordErrorsKeepBothCausesAfterClose(t *testing.T) {
	for _, stage := range []string{"mark", "finish", "commit"} {
		t.Run(stage, func(t *testing.T) {
			harness := newCatalogLoadHarness(t.Context())
			harness.stream.contents = []byte("<datafile><game></datafile>")
			switch stage {
			case "mark":
				harness.repository.markErr = errCatalogFailureRecord
			case "finish":
				harness.repository.finishErr = errCatalogFailureRecord
			case "commit":
				harness.repository.commitErr = errCatalogFailureRecord
			}
			_, err := harness.load(catalogExpectedStats(), 0)
			if !errors.Is(err, arcadedat.ErrInvalidDocument) || !errors.Is(err, errCatalogFailureRecord) {
				t.Fatalf("retained failure lost cause: %v", err)
			}
			want := []string{"open", "read", "close", "transaction", "mark-failed"}
			if stage != "mark" {
				want = append(want, "finish")
			}
			assertCatalogEvents(t, harness, want...)
			if harness.stream.closes != 1 {
				t.Fatalf("Close count = %d", harness.stream.closes)
			}
		})
	}
}

func TestCatalogConstructionRequiresSourceAndDiagnosticReporter(t *testing.T) {
	harness := newCatalogLoadHarness(t.Context())
	tests := []struct {
		name     string
		source   model.DATCatalogSource
		reporter diagnostics.ErrorReporter
	}{
		{"source", nil, harness.reporter},
		{"reporter", harness.source, nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewCatalogs accepted missing %s", testCase.name)
				}
			}()
			NewCatalogs(&runtime.Set{}, harness.repository, testCase.source, testCase.reporter)
		})
	}
}

func assertCatalogEvents(t *testing.T, harness *catalogLoadHarness, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(*harness.events, want) {
		t.Fatalf("resource and writer order = %v, want %v", *harness.events, want)
	}
}

func assertCatalogDiagnostic(t *testing.T, harness *catalogLoadHarness, errorType string) {
	t.Helper()
	want := []diagnostics.DiagnosticEvent{{
		Operation: "close", Code: diagnostics.CleanupFailureCode, Message: errorType,
	}}
	if harness.stream.closes != 1 || !reflect.DeepEqual(harness.reporter.reports, want) {
		t.Fatalf("Close/diagnostic count or sanitization: closes=%d events=%#v",
			harness.stream.closes, harness.reporter.reports)
	}
}
