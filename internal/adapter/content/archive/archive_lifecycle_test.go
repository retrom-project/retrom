package archive

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
	librarymodel "retrom/internal/model/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestProjectCursorRequiresExplicitCompleteAndClose(t *testing.T) {
	t.Parallel()
	for _, format := range []contentprofile.ArchiveFormat{
		contentprofile.ArchiveZIP, contentprofile.ArchiveNWJSExecutable, contentprofile.ArchiveElectronASAR,
	} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			path, body := lifecycleFixture(t, format)
			cursor, err := New(&testsupport.DiagnosticRecorder{}).OpenProject(t.Context(), path, format, importing.DefaultArchiveLimits())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := cursor.Close(); err != nil {
					t.Error(err)
				}
			}()
			assertBeforeNext(t, cursor)
			assertPayloadAndComplete(t, cursor, body)
			assertCompletedAndClosed(t, cursor, body)
		})
	}
}

func assertBeforeNext(t *testing.T, cursor librarymodel.ArchiveReader) {
	t.Helper()
	if _, err := cursor.Read(make([]byte, 1)); !errors.Is(err, errCursorState) {
		t.Fatalf("read before Next: %v", err)
	}
	if _, err := cursor.Complete(importing.ArchiveContent{}); !errors.Is(err, errCursorState) {
		t.Fatalf("complete before Next: %v", err)
	}
}

func assertPayloadAndComplete(t *testing.T, cursor librarymodel.ArchiveReader, body []byte) {
	t.Helper()
	header, err := cursor.Next()
	if err != nil {
		t.Fatal(err)
	}
	evidence := []string{header.Entry.CRC32, header.Entry.MD5, header.Entry.SHA1, header.Entry.SHA256}
	for _, value := range evidence {
		if value != "" {
			t.Fatalf("premature hashes: %#v", header)
		}
	}
	if header.Entry.Size != 0 {
		t.Fatalf("premature size: %#v", header)
	}
	if _, err := cursor.Next(); !errors.Is(err, errCursorState) {
		t.Fatalf("Next before Complete: %v", err)
	}
	// Exact byte consumption without observing EOF remains valid.
	got := make([]byte, len(body))
	if _, err := io.ReadFull(cursor, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("bytes=%q want %q", got, body)
	}
	entry, err := cursor.Complete(testArchiveContent(got))
	if err != nil || entry.Size != int64(len(got)) {
		t.Fatalf("complete=%#v %v", entry, err)
	}
}

func assertCompletedAndClosed(t *testing.T, cursor librarymodel.ArchiveReader, body []byte) {
	t.Helper()
	if _, err := cursor.Read(make([]byte, len(body))); !errors.Is(err, errCursorState) {
		t.Fatalf("read after Complete: %v", err)
	}
	if _, err := cursor.Complete(testArchiveContent(body)); !errors.Is(err, errCursorState) {
		t.Fatalf("second Complete: %v", err)
	}
	for range 2 {
		if _, err := cursor.Next(); !sameErrorObject(err, io.EOF) {
			t.Fatalf("terminal error %T %v, want bare EOF", err, err)
		}
	}
	for range 2 {
		if err := cursor.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cursor.Next(); !errors.Is(err, errCursorState) {
		t.Fatalf("Next after Close: %v", err)
	}
}

func lifecycleFixture(t *testing.T, format contentprofile.ArchiveFormat) (string, []byte) {
	t.Helper()
	body := []byte("single member")
	switch format {
	case contentprofile.ArchiveZIP:
		return writeZIP(t, "data.bin", body), body
	case contentprofile.ArchiveNWJSExecutable:
		return writeNWJSExecutable(t, true, true), []byte("<!doctype html>")
	case contentprofile.ArchiveElectronASAR:
		return writeElectronASARZIP(t, map[string][]byte{"data.bin": body}, nil, true), body
	case contentprofile.ArchiveSevenZip:
		t.Fatal("7z is outside this resource contract")
	}
	t.Fatalf("unknown test format %q", format)
	return "", nil
}

func TestOpenProjectFailureReturnsNilInterface(t *testing.T) {
	t.Parallel()
	factory := New(&testsupport.DiagnosticRecorder{})
	for _, format := range []contentprofile.ArchiveFormat{
		contentprofile.ArchiveZIP, contentprofile.ArchiveNWJSExecutable, contentprofile.ArchiveElectronASAR,
	} {
		cursor, err := factory.OpenProject(t.Context(), t.TempDir()+"/missing", format, importing.DefaultArchiveLimits())
		if err == nil || cursor != nil {
			t.Fatalf("%s resource=%#v error=%v", format, cursor, err)
		}
	}
}

func TestArchiveFactoryRequiresReporter(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("nil reporter was accepted")
		}
	}()
	New(nil)
}

type observedCloser struct {
	name     string
	events   *[]string
	resource io.Closer
	failure  error
	calls    int
}

func (closer *observedCloser) Close() error {
	closer.calls++
	*closer.events = append(*closer.events, closer.name)
	if closer.resource != nil {
		if err := closer.resource.Close(); err != nil {
			return err
		}
	}
	return closer.failure
}

type observedReader struct {
	reader io.Reader
	closer observedCloser
	reads  int
}

func (reader *observedReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

func (reader *observedReader) Close() error { return reader.closer.Close() }

type hostileCloseError struct{}

func (*hostileCloseError) Error() string { panic("private error text must not be evaluated") }

func assertCloseEvents(t *testing.T, events, want []string) {
	t.Helper()
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("close events=%v, want %v", events, want)
	}
}

func assertSanitizedCloseReports(t *testing.T, recorder *testsupport.DiagnosticRecorder, count int) {
	t.Helper()
	events := recorder.Events()
	if len(events) != count {
		t.Fatalf("diagnostic count=%d want %d", len(events), count)
	}
	want := diagnostics.CleanupFailure("close", "", "*archive.hostileCloseError")
	for _, event := range events {
		if event != want {
			t.Fatalf("diagnostic=%#v want %#v", event, want)
		}
	}
}

func TestResourceCloseIgnoresCancellationAndNeverFormatsError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var events []string
	recorder := &testsupport.DiagnosticRecorder{}
	closer := &observedCloser{name: "close", events: &events, failure: (*hostileCloseError)(nil)}
	closeResource(ctx, recorder, closer)
	assertCloseEvents(t, events, []string{"close"})
	assertSanitizedCloseReports(t, recorder, 1)
}

// Every expected error here is a pointer-backed sentinel. reflect.Value equality
// compares the actual object, rejecting even a same-text or same-type wrapper.
func sameErrorObject(got, want error) bool { return reflect.ValueOf(got) == reflect.ValueOf(want) }
