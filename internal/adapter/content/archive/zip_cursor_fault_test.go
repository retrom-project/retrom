package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testsupport"
)

func faultZIPCursor(t *testing.T, body []byte) (*zipCursor, *observedReader, *observedCloser, *[]string, *testsupport.DiagnosticRecorder) {
	t.Helper()
	recorder := &testsupport.DiagnosticRecorder{}
	factory := New(recorder)
	cursor, err := factory.openZIP(t.Context(), writeZIP(t, "data.bin", body), importing.DefaultArchiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	events := make([]string, 0, 2)
	member := &observedReader{reader: bytes.NewReader(body), closer: observedCloser{
		name: "member", events: &events, failure: &hostileCloseError{},
	}}
	cursor.archive.reader.RegisterDecompressor(zip.Deflate, func(io.Reader) io.ReadCloser { return member })
	outer := &observedCloser{name: "archive", events: &events, resource: cursor.archive.file, failure: &hostileCloseError{}}
	cursor.archive.file = outer
	t.Cleanup(func() {
		if err := cursor.Close(); err != nil {
			t.Error(err)
		}
	})
	return cursor, member, outer, &events, recorder
}

func TestZIPCompleteClosesMemberBeforeArchiveAndReportsOnce(t *testing.T) {
	t.Parallel()
	body := []byte("streamed")
	cursor, member, outer, events, recorder := faultZIPCursor(t, body)
	if _, err := cursor.Next(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(cursor)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cursor.Complete(testArchiveContent(got))
	if err != nil || entry.SHA256 == "" {
		t.Fatalf("complete=%#v %v", entry, err)
	}
	assertCloseEvents(t, *events, []string{"member"})
	if _, err := cursor.Next(); !sameErrorObject(err, io.EOF) {
		t.Fatalf("terminal=%v", err)
	}
	for range 2 {
		if err := cursor.Close(); err != nil {
			t.Fatal(err)
		}
	}
	assertCloseEvents(t, *events, []string{"member", "archive"})
	if member.closer.calls != 1 || outer.calls != 1 {
		t.Fatalf("close calls member=%d archive=%d", member.closer.calls, outer.calls)
	}
	assertSanitizedCloseReports(t, recorder, 2)
}

func TestZIPFailedCompleteRetainsDomainCauseAndClosesExactlyOnce(t *testing.T) {
	t.Parallel()
	cursor, member, outer, events, recorder := faultZIPCursor(t, []byte("streamed"))
	header, err := cursor.Next()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cursor.Complete(importing.ArchiveContent{})
	if entry != (importing.ArchiveEntry{}) || err == nil ||
		err.Error() != "scan archive entry \"data.bin\": ARCHIVE_UNSAFE" ||
		!sameErrorObject(errors.Unwrap(err), importing.ErrArchiveUnsafe) {
		t.Fatalf("completion=%#v error=%v", entry, err)
	}
	if _, err := cursor.Next(); !errors.Is(err, importing.ErrArchiveUnsafe) {
		t.Fatalf("failure not terminal: %v", err)
	}
	for range 2 {
		if err := cursor.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if header.Entry.Size != 0 || member.closer.calls != 1 || outer.calls != 1 {
		t.Fatal("header or close ownership changed")
	}
	assertCloseEvents(t, *events, []string{"member", "archive"})
	assertSanitizedCloseReports(t, recorder, 2)
}

func TestZIPCancellationLeavesResourcesForOwnerClose(t *testing.T) {
	t.Parallel()
	cursor, member, outer, events, recorder := faultZIPCursor(t, []byte("streamed"))
	ctx, cancel := context.WithCancel(t.Context())
	cursor.ctx = ctx
	header, err := cursor.Next()
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	var buffer [8]byte
	count, readErr := cursor.Read(buffer[:])
	if count != 0 || !errors.Is(readErr, context.Canceled) || member.reads != 0 {
		t.Fatalf("canceled read=%d %v reads=%d", count, readErr, member.reads)
	}
	if len(*events) != 0 {
		t.Fatal("Read closed owner resources before Stage returned")
	}
	closeErr := cursor.Close()
	stageErr := importing.ProjectArchiveStageError(header, readErr, closeErr)
	if !errors.Is(stageErr, context.Canceled) || stageErr.Error() != "scan archive entry \"data.bin\": importing/archive: context canceled" {
		t.Fatalf("stage=%v", stageErr)
	}
	if member.closer.calls != 1 || outer.calls != 1 {
		t.Fatal("owner close was lost")
	}
	assertCloseEvents(t, *events, []string{"member", "archive"})
	assertSanitizedCloseReports(t, recorder, 2)
}

func TestArchiveMonitorBoundAndErrorOrder(t *testing.T) {
	t.Parallel()
	cause := errors.New("read fault")
	raw := &oneReadFault{contents: []byte("abcd"), failure: cause}
	monitor := &archiveReadMonitor{ctx: t.Context(), reader: raw, limit: 3}
	var buffer [8]byte
	count, err := monitor.Read(buffer[:])
	if count != 0 || !sameErrorObject(err, importing.ErrArchiveLimitExceeded) || monitor.written != 0 {
		t.Fatalf("limit must precede accompanying read error: %d %v written=%d", count, err, monitor.written)
	}
	raw = &oneReadFault{contents: []byte("abc"), failure: cause}
	monitor = &archiveReadMonitor{ctx: t.Context(), reader: raw, limit: 3}
	count, err = monitor.Read(buffer[:])
	if count != 3 || !sameErrorObject(errors.Unwrap(err), cause) || monitor.written != 3 ||
		string(monitor.observedPrefix()) != "abc" {
		t.Fatalf("read cause/count changed: %d %v", count, err)
	}
}

type oneReadFault struct {
	contents []byte
	failure  error
}

func (reader *oneReadFault) Read(buffer []byte) (int, error) {
	count := copy(buffer, reader.contents)
	reader.contents = reader.contents[count:]
	return count, reader.failure
}
