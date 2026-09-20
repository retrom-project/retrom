package libraryimport

import (
	"context"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
)

func TestProjectArchiveRetainsOrderCandidatesAndInputs(t *testing.T) {
	t.Parallel()
	for _, format := range []contentprofile.ArchiveFormat{
		contentprofile.ArchiveZIP, contentprofile.ArchiveNWJSExecutable, contentprofile.ArchiveElectronASAR,
	} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			checkProjectArchiveSuccess(t, format)
		})
	}
}

func checkProjectArchiveSuccess(t *testing.T, format contentprofile.ArchiveFormat) {
	t.Helper()
	asar := format == contentprofile.ArchiveElectronASAR
	fixture := newProjectArchiveFixture(t,
		projectArchiveMember(7, "last.txt", asar, false),
		projectArchiveMember(2, "first.txt", asar, asar),
	)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	entries, candidates, err := fixture.service.scanProjectArchivePath(ctx, fixture.source, format)
	if err != nil {
		t.Fatal(err)
	}
	defer discardProjectArchiveCandidates(candidates)
	if len(entries) != 2 || entries[0].Ordinal != 2 || entries[1].Ordinal != 7 {
		t.Fatalf("original ordinals/order lost: %+v", entries)
	}
	if entries[0].NormalizedPath != "first.txt" || entries[1].NormalizedPath != "last.txt" {
		t.Fatalf("entries were relabeled during sorting: %+v", entries)
	}
	if len(candidates) != 2 || fixture.reader.closeCalls != 1 || fixture.reader.nextCalls != 3 {
		t.Fatalf("candidates=%v close=%d next=%d", candidates, fixture.reader.closeCalls, fixture.reader.nextCalls)
	}
	assertProjectArchiveCompletions(t, fixture.reader, 2)
	assertProjectArchiveCandidates(t, entries, candidates)
	assertProjectArchiveOpenCall(ctx, t, fixture, format)
	assertProjectArchiveUnpublished(t, fixture)
	if len(fixture.diagnostics.calls) != 0 {
		t.Fatalf("unexpected cleanup diagnostics: %+v", fixture.diagnostics.calls)
	}
	discardProjectArchiveCandidates(candidates)
	assertProjectArchiveClean(t, fixture)
}

func assertProjectArchiveOpenCall(
	ctx context.Context, t *testing.T, fixture *projectArchiveFixture, format contentprofile.ArchiveFormat,
) {
	t.Helper()
	if len(fixture.opener.calls) != 1 {
		t.Fatalf("OpenProject calls=%d", len(fixture.opener.calls))
	}
	call := fixture.opener.calls[0]
	limits := importing.ArchiveLimits{
		MaxEntries: 20000, MaxEntryBytes: 8 << 30, MaxExpandedBytes: 32 << 30,
		MaxCompressionRatio: 200, AllowNestedArchives: true,
	}
	if call.context != ctx || call.path != fixture.source || call.format != format || call.limits != limits {
		t.Fatalf("changed acquire facts: call=%+v sameContext=%v wantFormat=%s limits=%+v",
			call, call.context == ctx, format, limits)
	}
}

func TestProjectArchiveEmptyZIPRetainsNonNilResultWithCanceledContext(t *testing.T) {
	t.Parallel()
	fixture := newProjectArchiveFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	entries, candidates, err := fixture.service.scanProjectArchivePath(ctx, fixture.source, contentprofile.ArchiveZIP)
	if err != nil || entries == nil || len(entries) != 0 || candidates == nil || len(candidates) != 0 {
		t.Fatalf("empty ZIP changed: entries=%#v candidates=%#v err=%v", entries, candidates, err)
	}
	if fixture.reader.closeCalls != 1 || fixture.reader.nextCalls != 1 || fixture.reader.completeCalls != 0 {
		t.Fatalf("empty resource lifecycle: %+v", fixture.reader)
	}
	assertProjectArchiveOpenCall(ctx, t, fixture, contentprofile.ArchiveZIP)
	assertProjectArchiveClean(t, fixture)
}
