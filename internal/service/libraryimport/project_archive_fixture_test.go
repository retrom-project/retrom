package libraryimport

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
	model "retrom/internal/model/libraryimport"
)

var errProjectArchiveFixtureState = errors.New("invalid test archive state")

type projectArchiveTestMember struct {
	header          importing.ArchiveMemberHeader
	body            string
	readFailure     error
	completeFailure error
}

type projectArchiveStageObservation struct {
	paths []string
	bytes []string
}

type projectArchiveReaderRecord struct {
	test                                 *testing.T
	stageDir                             string
	members                              []projectArchiveTestMember
	current                              int
	active                               *strings.Reader
	closed                               bool
	nextCalls, completeCalls, closeCalls int
	nextFailures                         map[int]error
	closeFailure                         error
	contents                             []importing.ArchiveContent
	observations                         []projectArchiveStageObservation
}

func (reader *projectArchiveReaderRecord) Next() (importing.ArchiveMemberHeader, error) {
	reader.nextCalls++
	if reader.closed || reader.active != nil {
		return importing.ArchiveMemberHeader{}, errProjectArchiveFixtureState
	}
	if failure := reader.nextFailures[reader.nextCalls]; failure != nil {
		return importing.ArchiveMemberHeader{}, failure
	}
	if reader.current+1 == len(reader.members) {
		return importing.ArchiveMemberHeader{}, io.EOF
	}
	reader.current++
	member := reader.members[reader.current]
	reader.active = strings.NewReader(member.body)
	return member.header, nil
}

func (reader *projectArchiveReaderRecord) Read(buffer []byte) (int, error) {
	if reader.closed || reader.active == nil {
		return 0, errProjectArchiveFixtureState
	}
	count, err := reader.active.Read(buffer)
	if errors.Is(err, io.EOF) && reader.members[reader.current].readFailure != nil {
		return count, reader.members[reader.current].readFailure
	}
	return count, err
}

func (reader *projectArchiveReaderRecord) Complete(content importing.ArchiveContent) (importing.ArchiveEntry, error) {
	if reader.closed || reader.active == nil {
		return importing.ArchiveEntry{}, errProjectArchiveFixtureState
	}
	reader.completeCalls++
	reader.contents = append(reader.contents, content)
	observation := projectArchiveStageObservation{paths: projectArchiveTemporaryPaths(reader.test, reader.stageDir)}
	for _, path := range observation.paths {
		data, err := os.ReadFile(path)
		if err != nil {
			reader.test.Fatal(err)
		}
		observation.bytes = append(observation.bytes, string(data))
	}
	reader.observations = append(reader.observations, observation)
	member := reader.members[reader.current]
	reader.active = nil
	if member.completeFailure != nil {
		return importing.ArchiveEntry{}, member.completeFailure
	}
	entry := member.header.Entry
	entry.Size, entry.CRC32, entry.MD5 = content.Size, content.CRC32, content.MD5
	entry.SHA1, entry.SHA256 = content.SHA1, content.SHA256
	return entry, nil
}

func (reader *projectArchiveReaderRecord) Close() error {
	reader.closeCalls++
	reader.closed = true
	reader.active = nil
	return reader.closeFailure
}

type projectArchiveOpenCall struct {
	context context.Context
	path    string
	format  contentprofile.ArchiveFormat
	limits  importing.ArchiveLimits
}

type projectArchiveOpenerRecord struct {
	reader  model.ArchiveReader
	failure error
	calls   []projectArchiveOpenCall
}

func (opener *projectArchiveOpenerRecord) OpenProject(
	ctx context.Context, path string, format contentprofile.ArchiveFormat, limits importing.ArchiveLimits,
) (model.ArchiveReader, error) {
	opener.calls = append(opener.calls, projectArchiveOpenCall{context: ctx, path: path, format: format, limits: limits})
	if opener.failure != nil {
		return nil, opener.failure
	}
	return opener.reader, nil
}

type projectArchiveDiagnosticCall struct {
	context context.Context
	event   diagnostics.DiagnosticEvent
}

type projectArchiveDiagnosticRecord struct {
	calls []projectArchiveDiagnosticCall
}

func (record *projectArchiveDiagnosticRecord) Report(ctx context.Context, event diagnostics.DiagnosticEvent) {
	record.calls = append(record.calls, projectArchiveDiagnosticCall{context: ctx, event: event})
}

type projectArchiveFixture struct {
	root, stageDir, source string
	service                *ImportPreparation
	reader                 *projectArchiveReaderRecord
	opener                 *projectArchiveOpenerRecord
	diagnostics            *projectArchiveDiagnosticRecord
}

func newProjectArchiveFixture(t *testing.T, members ...projectArchiveTestMember) *projectArchiveFixture {
	t.Helper()
	root := t.TempDir()
	store, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(root, "tmp", "jobs")
	reader := &projectArchiveReaderRecord{test: t, stageDir: stageDir, members: members, current: -1}
	opener := &projectArchiveOpenerRecord{reader: reader}
	reporter := &projectArchiveDiagnosticRecord{}
	return &projectArchiveFixture{
		root: root, stageDir: stageDir, source: filepath.Join(root, "source.zip"),
		reader: reader, opener: opener, diagnostics: reporter,
		service: NewImportPreparation(nil, nil, store, ImportPreparationOptions{
			ProjectArchives: opener, Diagnostics: reporter,
		}),
	}
}

func projectArchiveMember(ordinal int, name string, asar, unpacked bool) projectArchiveTestMember {
	format := "ZIP"
	if asar {
		format = "ELECTRON_ASAR"
	}
	return projectArchiveTestMember{
		header: importing.ArchiveMemberHeader{
			Entry: importing.ArchiveEntry{
				Ordinal: ordinal, OriginalPath: name, NormalizedPath: name,
				ASCIICasefoldPath: name, ArchiveFormat: format,
			},
			Unpacked: unpacked,
		},
		body: "abc",
	}
}

func projectArchiveABCContent() importing.ArchiveContent {
	return importing.ArchiveContent{
		Size: 3, CRC32: "352441c2", MD5: "900150983cd24fb0d6963f7d28e17f72",
		SHA1:   "a9993e364706816aba3e25717850c26c9cd0d89d",
		SHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	}
}

func projectArchiveTemporaryPaths(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), ".blob-") {
			t.Fatalf("unexpected staging entry %s", entry.Name())
		}
		paths = append(paths, filepath.Join(directory, entry.Name()))
	}
	return paths
}

func assertProjectArchiveUnpublished(t *testing.T, fixture *projectArchiveFixture) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixture.root, "blobs", "sha256"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("CAS unexpectedly published: entries=%v err=%v", entries, err)
	}
}

func assertProjectArchiveClean(t *testing.T, fixture *projectArchiveFixture) {
	t.Helper()
	if paths := projectArchiveTemporaryPaths(t, fixture.stageDir); len(paths) != 0 {
		t.Fatalf("staged candidates leaked: %v", paths)
	}
	for _, observation := range fixture.reader.observations {
		for _, path := range observation.paths {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate survived cleanup: %s err=%v", path, err)
			}
		}
	}
	assertProjectArchiveUnpublished(t, fixture)
}

func assertProjectArchiveCompletions(t *testing.T, reader *projectArchiveReaderRecord, count int) {
	t.Helper()
	if reader.completeCalls != count || len(reader.contents) != count || len(reader.observations) != count {
		t.Fatalf("Complete calls=%d contents=%v observations=%v", reader.completeCalls, reader.contents, reader.observations)
	}
	for index, content := range reader.contents {
		if content != projectArchiveABCContent() {
			t.Fatalf("Complete[%d] content=%+v", index, content)
		}
		observation := reader.observations[index]
		if len(observation.paths) != index+1 || len(observation.bytes) != index+1 {
			t.Fatalf("Complete[%d] did not see registered candidate files: %+v", index, observation)
		}
		for _, data := range observation.bytes {
			if data != "abc" {
				t.Fatalf("Complete[%d] staged bytes=%q", index, data)
			}
		}
	}
}

func assertProjectArchiveCandidates(
	t *testing.T, entries []importing.ArchiveEntry, candidates map[int]*blobstore.Candidate,
) {
	t.Helper()
	for _, entry := range entries {
		candidate := candidates[entry.Ordinal]
		if candidate == nil {
			t.Fatalf("missing candidate ordinal %d", entry.Ordinal)
		}
		metadata := candidate.Metadata()
		if metadata.Size != entry.Size || metadata.CRC32 != entry.CRC32 || metadata.MD5 != entry.MD5 ||
			metadata.SHA1 != entry.SHA1 || metadata.SHA256 != entry.SHA256 {
			t.Fatalf("entry/candidate metadata differ: entry=%+v metadata=%+v", entry, metadata)
		}
		data, readErr := os.ReadFile(candidate.Path())
		if readErr != nil || string(data) != "abc" {
			t.Fatalf("successful candidate was removed or changed: data=%q err=%v", data, readErr)
		}
	}
}
