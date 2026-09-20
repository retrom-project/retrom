package archive

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"testing"

	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testsupport"
)

func faultUnpackedCursor(t *testing.T, body []byte, closeErr error) (*asarCursor, *observedReader, *observedCloser, *[]string, *testsupport.DiagnosticRecorder) {
	t.Helper()
	events := make([]string, 0, 3)
	member := &observedReader{reader: bytes.NewReader(body), closer: observedCloser{name: "unpacked", events: &events, failure: closeErr}}
	app := &observedReader{reader: bytes.NewReader(nil), closer: observedCloser{name: "app.asar", events: &events, failure: &hostileCloseError{}}}
	outer := &observedCloser{name: "archive", events: &events, failure: &hostileCloseError{}}
	recorder := &testsupport.DiagnosticRecorder{}
	facts := testArchiveContent(body)
	cursor := &asarCursor{
		ctx: t.Context(), reporter: recorder,
		archive: &electronZIPArchive{archive: zipArchive{file: outer}, layout: importing.ElectronZIPLayout{
			Unpacked: map[string]importing.ZIPMember{"u.node": {Header: importing.ZIPHeaderFacts{CRC32: parseTestCRC(t, facts.CRC32)}}},
		}},
		appReader: app, unpackedReader: member, active: true,
		members: []importing.ASARMember{{Path: "u.node", Size: int64(len(body)), Unpacked: true}},
		monitor: &archiveReadMonitor{ctx: t.Context(), reader: io.LimitReader(member, int64(len(body))), limit: int64(len(body))},
	}
	t.Cleanup(func() {
		if err := cursor.Close(); err != nil {
			t.Error(err)
		}
	})
	return cursor, member, outer, &events, recorder
}

func TestASARUnpackedCompleteFailureCombinesThenClosesOuterResources(t *testing.T) {
	t.Parallel()
	closeCause := errors.New("member close fault")
	cursor, member, outer, events, recorder := faultUnpackedCursor(t, []byte("abc"), closeCause)
	entry, err := cursor.Complete(importing.ArchiveContent{})
	if entry != (importing.ArchiveEntry{}) || !errors.Is(err, closeCause) ||
		!errors.Is(err, importing.ErrElectronASARInvalid) {
		t.Fatalf("completion=%#v error=%v", entry, err)
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("completion lost Join type: %T", err)
	}
	causes := joined.Unwrap()
	if len(causes) != 3 || !sameErrorObject(causes[0], importing.ErrElectronASARInvalid) ||
		!sameErrorObject(causes[1], closeCause) || !sameErrorObject(causes[2], importing.ErrElectronASARInvalid) {
		t.Fatalf("cause order=%v", causes)
	}
	if member.reads != 0 {
		t.Fatal("invalid content must skip EOF read")
	}
	assertCloseEvents(t, *events, []string{"unpacked"})
	for range 2 {
		if err := cursor.Close(); err != nil {
			t.Fatal(err)
		}
	}
	assertCloseEvents(t, *events, []string{"unpacked", "app.asar", "archive"})
	if member.closer.calls != 1 || outer.calls != 1 {
		t.Fatal("resources closed more than once")
	}
	assertSanitizedCloseReports(t, recorder, 2)
}

func TestASARStageFailureGetsUnpackedCloseCauseWithoutDuplicateReport(t *testing.T) {
	t.Parallel()
	stageCause := errors.New("stage fault")
	closeCause := errors.New("member close fault")
	cursor, member, outer, events, recorder := faultUnpackedCursor(t, []byte("abc"), closeCause)
	header := cursor.members[0].Header(0)
	closeErr := cursor.Close()
	if !sameErrorObject(closeErr, closeCause) {
		t.Fatalf("Close replaced its raw error object: %T", closeErr)
	}
	err := importing.ProjectArchiveStageError(header, stageCause, closeErr)
	if err.Error() != "stage fault\nmember close fault\nARCHIVE_UNSAFE: ELECTRON_ASAR_INVALID" {
		t.Fatalf("stage error order=%v", err)
	}
	if !errors.Is(err, stageCause) || !errors.Is(err, closeCause) || !errors.Is(err, importing.ErrElectronASARInvalid) {
		t.Fatalf("stage causes lost: %v", err)
	}
	if err := cursor.Close(); err != nil {
		t.Fatal(err)
	}
	if member.closer.calls != 1 || outer.calls != 1 {
		t.Fatal("duplicate Close")
	}
	assertCloseEvents(t, *events, []string{"unpacked", "app.asar", "archive"})
	assertSanitizedCloseReports(t, recorder, 2)
}

func TestASARUnpackedChecksEOFBeforeCloseAndCRC(t *testing.T) {
	t.Parallel()
	cursor, member, _, events, _ := faultUnpackedCursor(t, []byte("abc"), nil)
	got, err := io.ReadAll(cursor)
	if err != nil {
		t.Fatal(err)
	}
	before := member.reads
	wrongCRC := testArchiveContent(got)
	wrongCRC.CRC32 = "00000000"
	_, err = cursor.Complete(wrongCRC)
	if !sameErrorObject(err, importing.ErrElectronASARInvalid) || member.reads != before+1 {
		t.Fatalf("CRC/EOF order: %v reads=%d before=%d", err, member.reads, before)
	}
	assertCloseEvents(t, *events, []string{"unpacked"})
}

func TestASARPackedStageFailureKeepsOriginalIdentity(t *testing.T) {
	t.Parallel()
	primary := errors.New("stage fault")
	header := importing.ArchiveMemberHeader{Entry: importing.ArchiveEntry{ArchiveFormat: "ELECTRON_ASAR"}}
	if got := importing.ProjectArchiveStageError(header, primary, errors.New("ignored outer close")); !sameErrorObject(got, primary) {
		t.Fatalf("packed stage identity changed: %T", got)
	}
}

func parseTestCRC(t *testing.T, value string) uint32 {
	t.Helper()
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(parsed)
}
