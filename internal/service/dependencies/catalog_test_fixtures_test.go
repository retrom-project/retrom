package dependencies

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	runtime "retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/arcadedat"
	model "retrom/internal/model/dependencies"
	"retrom/internal/model/diagnostics"
)

const catalogFixtureXML = "<datafile><game name=\"demo\"><rom name=\"demo.bin\" size=\"1\" crc=\"12345678\"/></game></datafile>"

type catalogReplayCase struct {
	Name                          string
	Contents                      string
	Missing, Cancel, WriteFailure bool
	Core                          string
	Expected                      model.CatalogStats
	Indexed                       int64
}

func catalogReplayCases() []catalogReplayCase {
	expected := model.CatalogStats{MachineCount: 1, ROMEntryCount: 1}
	return []catalogReplayCase{
		{Name: "valid", Contents: catalogFixtureXML, Expected: expected},
		{Name: "indexed-shortcut-missing", Missing: true, Expected: expected, Indexed: 1},
		{Name: "open-missing", Missing: true, Expected: expected},
		{Name: "open-missing-write-failed", Missing: true, WriteFailure: true, Expected: expected},
		{Name: "canceled-open-missing", Missing: true, Cancel: true, Expected: expected},
		{Name: "statistics-mismatch", Contents: catalogFixtureXML, Expected: model.CatalogStats{MachineCount: 2}},
		{Name: "statistics-mismatch-write-failed", Contents: catalogFixtureXML, Expected: model.CatalogStats{MachineCount: 2}, WriteFailure: true},
		{Name: "invalid-xml", Contents: "<datafile><game></datafile>", Expected: expected},
		{Name: "invalid-xml-write-failed", Contents: "<datafile><game></datafile>", Expected: expected, WriteFailure: true},
		{Name: "empty-dat", Contents: "", Expected: expected},
		{Name: "canceled-valid", Contents: catalogFixtureXML, Cancel: true, Expected: expected},
		{Name: "canceled-valid-write-failed", Contents: catalogFixtureXML, Cancel: true, WriteFailure: true, Expected: expected},
		{Name: "unsupported-core", Contents: catalogFixtureXML, Core: "unsupported", Expected: expected},
	}
}

type catalogErrorNode struct {
	Type     string
	Text     string
	Children []catalogErrorNode
}

type catalogObservation struct {
	Name                          string
	Catalog                       arcadedat.Catalog
	Error                         *catalogErrorNode
	Events                        []string
	Code                          string
	Canceled, Missing, WriteCause bool
}

func describeCatalogError(err error, root string) *catalogErrorNode {
	if err == nil {
		return nil
	}
	result := &catalogErrorNode{Type: fmt.Sprintf("%T", err), Text: err.Error()}
	if root != "" {
		result.Text = strings.ReplaceAll(result.Text, root, "<ROOT>")
	}
	if child := errors.Unwrap(err); child != nil {
		result.Children = append(result.Children, *describeCatalogError(child, root))
		return result
	}
	// As alone may find a descendant join. Inspect this node's method set first
	// so extracting children preserves every wrapper and never invokes custom As.
	var cause interface{ Unwrap() []error }
	if reflect.TypeOf(err).Implements(reflect.TypeFor[interface{ Unwrap() []error }]()) && errors.As(err, &cause) {
		for _, child := range cause.Unwrap() {
			result.Children = append(result.Children, *describeCatalogError(child, root))
		}
	}
	return result
}

type catalogRepositoryProbe struct {
	model.Repository
	model.CatalogRecords
	model.JobRecords
	events                    *[]string
	transactionErr, commitErr error
	markErr, finishErr        error
	code                      string
}

func (repository *catalogRepositoryProbe) WithWrite(_ context.Context, work func(model.WriteScope) error) error {
	*repository.events = append(*repository.events, "transaction")
	if repository.transactionErr != nil {
		return repository.transactionErr
	}
	if err := work(model.WriteScope{Catalog: repository, Jobs: repository}); err != nil {
		return err
	}
	return repository.commitErr
}

func (repository *catalogRepositoryProbe) MarkFailed(context.Context, string, int64) error {
	*repository.events = append(*repository.events, "mark-failed")
	return repository.markErr
}

func (repository *catalogRepositoryProbe) Finish(_ context.Context, finish model.JobFinish) error {
	*repository.events = append(*repository.events, "finish")
	repository.code = finish.Code
	return repository.finishErr
}

type catalogSourceProbe struct {
	events         *[]string
	stream         *catalogStreamProbe
	openErr        error
	root, relative string
}

func (source *catalogSourceProbe) OpenBuiltIn(root, relative string) (io.ReadCloser, error) {
	*source.events = append(*source.events, "open")
	source.root, source.relative = root, relative
	if source.openErr != nil {
		return nil, source.openErr
	}
	return source.stream, nil
}

type catalogStreamProbe struct {
	events            *[]string
	contents          []byte
	readErr, closeErr error
	reads, closes     int
	closed            bool
}

func (stream *catalogStreamProbe) Read(buffer []byte) (int, error) {
	if stream.closed {
		panic("catalog source read after Close")
	}
	stream.reads++
	if stream.reads == 1 {
		*stream.events = append(*stream.events, "read")
	}
	count := copy(buffer, stream.contents)
	stream.contents = stream.contents[count:]
	if len(stream.contents) != 0 {
		return count, nil
	}
	if stream.readErr != nil {
		return count, stream.readErr
	}
	return count, io.EOF
}

func (stream *catalogStreamProbe) Close() error {
	stream.closes++
	stream.closed = true
	*stream.events = append(*stream.events, "close")
	return stream.closeErr
}

type catalogDiagnosticProbe struct {
	events   *[]string
	reports  []diagnostics.DiagnosticEvent
	contexts []context.Context
}

func (reporter *catalogDiagnosticProbe) Report(ctx context.Context, event diagnostics.DiagnosticEvent) {
	*reporter.events = append(*reporter.events, "report")
	reporter.reports = append(reporter.reports, event)
	reporter.contexts = append(reporter.contexts, ctx)
}

type catalogLoadHarness struct {
	bootstrap  catalogBootstrap
	events     *[]string
	source     *catalogSourceProbe
	stream     *catalogStreamProbe
	repository *catalogRepositoryProbe
	reporter   *catalogDiagnosticProbe
}

func newCatalogLoadHarness(ctx context.Context) *catalogLoadHarness {
	events := new([]string)
	stream := &catalogStreamProbe{events: events, contents: []byte(catalogFixtureXML)}
	source := &catalogSourceProbe{events: events, stream: stream}
	repository := &catalogRepositoryProbe{events: events}
	reporter := &catalogDiagnosticProbe{events: events}
	return &catalogLoadHarness{
		events: events, source: source, stream: stream, repository: repository, reporter: reporter,
		bootstrap: catalogBootstrap{
			ctx: ctx, repository: repository, source: source, reporter: reporter, now: time.UnixMilli(1000),
		},
	}
}

func (harness *catalogLoadHarness) load(expected model.CatalogStats, indexed int64) (arcadedat.Catalog, error) {
	return harness.bootstrap.loadCatalog(
		&runtime.Version{DATRoot: "frozen-dat-root"}, "fbneo", "fbneo/fixture.dat", expected, indexed, "dat", "job",
	)
}

func catalogExpectedStats() model.CatalogStats {
	return model.CatalogStats{MachineCount: 1, ROMEntryCount: 1}
}

type catalogPoisonCloseError struct{}

func (*catalogPoisonCloseError) Error() string { panic("private close error must not be evaluated") }

func (*catalogPoisonCloseError) Format(fmt.State, rune) {
	panic("private close error must not be formatted")
}

var errCatalogFailureRecord = errors.New("DAT failure record unavailable")
