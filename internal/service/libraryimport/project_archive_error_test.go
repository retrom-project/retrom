package libraryimport

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
)

func TestProjectArchiveCompleteFailureDiscardsRegisteredCandidates(t *testing.T) {
	t.Parallel()
	first := projectArchiveMember(7, "first.txt", true, false)
	second := projectArchiveMember(2, "second.txt", true, true)
	cause := errors.New("member integrity failure")
	second.completeFailure = cause
	fixture := newProjectArchiveFixture(t, first, second)
	closeCause := errors.New("close failed for /private/archive/path")
	fixture.reader.closeFailure = closeCause
	ctx := t.Context()
	entries, candidates, err := fixture.service.scanProjectArchivePath(ctx, fixture.source, contentprofile.ArchiveElectronASAR)
	if entries != nil || candidates != nil || !errors.Is(err, cause) {
		t.Fatalf("result survived failed Complete: entries=%v candidates=%v err=%v", entries, candidates, err)
	}
	if err.Error() != "libraryimport/RPG Maker archive: member integrity failure" ||
		errors.Is(err, closeCause) || errors.Is(err, importing.ErrElectronASARInvalid) {
		t.Fatalf("Complete error incorrectly used Stage mapping: %v", err)
	}
	if fixture.reader.closeCalls != 1 {
		t.Fatalf("Close calls=%d", fixture.reader.closeCalls)
	}
	assertProjectArchiveCompletions(t, fixture.reader, 2)
	assertProjectArchiveClean(t, fixture)
	if len(fixture.diagnostics.calls) != 1 {
		t.Fatalf("nonfatal Close report count=%d", len(fixture.diagnostics.calls))
	}
	report := fixture.diagnostics.calls[0]
	if report.context != ctx || report.event.Operation != "close" || report.event.Code != "cleanup.failed" ||
		report.event.Message != "*errors.errorString" || report.event.RequestID != "" {
		t.Fatalf("cleanup facts changed or exposed resource error: %+v", report)
	}
}

func TestProjectArchiveNextFailureDiscardsEarlierCandidates(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		failure error
	}{
		{"next-validation", errors.New("archive trailer invalid")},
		{"wrapped-eof", fmt.Errorf("archive trailer invalid: %w", io.EOF)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newProjectArchiveFixture(t, projectArchiveMember(5, "file.txt", false, false))
			fixture.reader.nextFailures = map[int]error{2: test.failure}
			entries, candidates, err := fixture.service.scanProjectArchivePath(t.Context(), fixture.source, contentprofile.ArchiveZIP)
			if entries != nil || candidates != nil || !errors.Is(err, test.failure) {
				t.Fatalf("Next error treated as success: entries=%v candidates=%v err=%v", entries, candidates, err)
			}
			if err.Error() != "libraryimport/RPG Maker archive: "+test.failure.Error() {
				t.Fatalf("Next error incorrectly remapped: %v", err)
			}
			if fixture.reader.closeCalls != 1 || fixture.reader.nextCalls != 2 {
				t.Fatalf("Close=%d Next=%d", fixture.reader.closeCalls, fixture.reader.nextCalls)
			}
			assertProjectArchiveCompletions(t, fixture.reader, 1)
			assertProjectArchiveClean(t, fixture)
		})
	}
}

type projectArchiveStageCase struct {
	name           string
	format         contentprofile.ArchiveFormat
	asar, unpacked bool
}

func TestProjectArchiveStageFailureRetainsFormatErrorPrecedence(t *testing.T) {
	t.Parallel()
	cases := []projectArchiveStageCase{
		{"zip", contentprofile.ArchiveZIP, false, false},
		{"nwjs", contentprofile.ArchiveNWJSExecutable, false, false},
		{"asar-packed", contentprofile.ArchiveElectronASAR, true, false},
		{"asar-unpacked", contentprofile.ArchiveElectronASAR, true, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkProjectArchiveStageFailure(t, test)
		})
	}
}

func checkProjectArchiveStageFailure(t *testing.T, test projectArchiveStageCase) {
	t.Helper()
	first := projectArchiveMember(6, "good.txt", test.asar, false)
	second := projectArchiveMember(1, "broken.txt", test.asar, test.unpacked)
	readCause := errors.New("source read failed")
	closeCause := errors.New("member close failed")
	second.readFailure = readCause
	fixture := newProjectArchiveFixture(t, first, second)
	fixture.reader.closeFailure = closeCause
	entries, candidates, err := fixture.service.scanProjectArchivePath(t.Context(), fixture.source, test.format)
	if entries != nil || candidates != nil || !errors.Is(err, readCause) {
		t.Fatalf("Stage failure lost: entries=%v candidates=%v err=%v", entries, candidates, err)
	}
	stage := "stage project entry: write blob candidate: source read failed"
	if !test.asar {
		stage = "scan archive entry \"broken.txt\": " + stage
	}
	if test.unpacked {
		stage += "\nmember close failed\nARCHIVE_UNSAFE: ELECTRON_ASAR_INVALID"
	}
	if err.Error() != "libraryimport/RPG Maker archive: "+stage {
		t.Fatalf("Stage mapping changed: got=%q want=%q", err, stage)
	}
	if errors.Is(err, closeCause) != test.unpacked || errors.Is(err, importing.ErrElectronASARInvalid) != test.unpacked {
		t.Fatalf("format-specific secondary causes changed: %v", err)
	}
	if test.unpacked {
		assertProjectArchiveJoinedCauses(t, errors.Unwrap(err), readCause, closeCause)
	}
	if fixture.reader.closeCalls != 1 || fixture.reader.nextCalls != 2 {
		t.Fatalf("Close=%d Next=%d", fixture.reader.closeCalls, fixture.reader.nextCalls)
	}
	assertProjectArchiveCompletions(t, fixture.reader, 1)
	assertProjectArchiveClean(t, fixture)
}

func assertProjectArchiveJoinedCauses(t *testing.T, err, readCause, closeCause error) {
	t.Helper()
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("ASAR unpacked failure lost joined causes: %T %v", err, err)
	}
	causes := joined.Unwrap()
	if len(causes) != 3 || !errors.Is(causes[0], readCause) ||
		reflect.ValueOf(causes[1]) != reflect.ValueOf(closeCause) ||
		reflect.ValueOf(causes[2]) != reflect.ValueOf(importing.ErrElectronASARInvalid) {
		t.Fatalf("joined primary/close/ASAR order changed: %v", causes)
	}
}

func TestProjectArchiveUnknownFormatDoesNotAcquire(t *testing.T) {
	t.Parallel()
	fixture := newProjectArchiveFixture(t)
	fixture.opener.failure = importing.ErrArchiveMethodUnsupported
	entries, candidates, err := fixture.service.scanProjectArchivePath(t.Context(), fixture.source, contentprofile.ArchiveFormat("UNKNOWN"))
	if entries != nil || candidates != nil || !errors.Is(err, importing.ErrArchiveUnsafe) ||
		err.Error() != "libraryimport/RPG Maker archive: ARCHIVE_UNSAFE" {
		t.Fatalf("unknown format compatibility changed: entries=%v candidates=%v err=%v", entries, candidates, err)
	}
	if len(fixture.opener.calls) != 0 || fixture.reader.closeCalls != 0 {
		t.Fatalf("unknown format acquired resource: opens=%v closes=%d", fixture.opener.calls, fixture.reader.closeCalls)
	}
	assertProjectArchiveClean(t, fixture)
}

func TestProjectArchiveAcquireFailureDoesNotCloseUnownedResource(t *testing.T) {
	t.Parallel()
	fixture := newProjectArchiveFixture(t)
	cause := errors.New("archive acquisition failed")
	fixture.opener.failure = cause
	ctx := t.Context()
	entries, candidates, err := fixture.service.scanProjectArchivePath(ctx, fixture.source, contentprofile.ArchiveZIP)
	if entries != nil || candidates != nil || !errors.Is(err, cause) ||
		err.Error() != "libraryimport/RPG Maker archive: archive acquisition failed" {
		t.Fatalf("acquire failure changed: entries=%v candidates=%v err=%v", entries, candidates, err)
	}
	if fixture.reader.closeCalls != 0 || fixture.reader.nextCalls != 0 {
		t.Fatalf("unowned reader used: Close=%d Next=%d", fixture.reader.closeCalls, fixture.reader.nextCalls)
	}
	assertProjectArchiveOpenCall(ctx, t, fixture, contentprofile.ArchiveZIP)
	assertProjectArchiveClean(t, fixture)
}
