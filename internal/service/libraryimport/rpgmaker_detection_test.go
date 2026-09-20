package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"

	archiveadapter "retrom/internal/adapter/content/archive"
	cleanupadapter "retrom/internal/adapter/system/cleanup"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/rpgmaker/detector"
	model "retrom/internal/model/libraryimport"
)

type preparedRPGProbeRecorder struct {
	t           *testing.T
	wantContext context.Context
	calls       int
	coreID      string
	files       []model.RPGMakerProbeFile
	fail        error
}

func (record *preparedRPGProbeRecorder) DetectPrepared(
	ctx context.Context, coreID string, files []model.RPGMakerProbeFile,
) (detector.Profile, error) {
	record.t.Helper()
	if ctx != record.wantContext {
		record.t.Fatal("detector did not receive the caller context")
	}
	record.calls++
	record.coreID = coreID
	record.files = append([]model.RPGMakerProbeFile(nil), files...)
	for _, file := range files {
		contents, err := os.ReadFile(file.SourcePath)
		if err != nil {
			record.t.Fatalf("prepared source %q is not alive during detection: %v", file.File.Path, err)
		}
		if int64(len(contents)) != file.File.Size {
			record.t.Fatalf("prepared size=%d, want %d", len(contents), file.File.Size)
		}
	}
	if record.fail != nil {
		return detector.Profile{}, record.fail
	}
	return detector.Profile{SelectedCoreID: coreID, ExpectedGeneration: detector.RPG2000}, nil
}

func TestRPGMakerPreparedPortKeepsPathsAliveAndPreservesErrors(t *testing.T) {
	t.Parallel()
	for _, sourceType := range []string{"DIRECTORY", "FILES"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fail=%v", sourceType, fail), func(t *testing.T) {
				t.Parallel()
				checkPreparedRPGPort(t, sourceType, fail)
			})
		}
	}
}

func preparedRPGProbeInputs(t *testing.T, store *blobstore.Store, sourceType string) []model.ImportFile {
	t.Helper()
	names := []string{"Map0001.lmu", "RPG_RT.ldb", "RPG_RT.lmt", "Save01.lsd", ".DS_Store"}
	bodies := []string{"map", "database", "tree", "save", "noise"}
	inputs := make([]model.ImportFile, 0, len(names))
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for i, name := range names {
		logical := path.Join("Wrapper", name)
		if sourceType == "DIRECTORY" {
			inputs = append(inputs, putPreparedRPGSource(t, store, logical, bodies[i]))
		} else {
			entry, err := writer.Create(logical)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(bodies[i])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if sourceType == "FILES" {
		blob, err := store.Put(bytes.NewReader(archive.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		return []model.ImportFile{{Path: "public-project.zip", SHA256: blob.SHA256, Size: blob.Size}}
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	return inputs
}

func TestRPGMakerPreparedPortRejectsUnconfiguredOrUnsupportedInput(t *testing.T) {
	t.Parallel()
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record := &preparedRPGProbeRecorder{t: t, wantContext: t.Context()}
	for _, service := range []*ImportPreparation{
		NewImportPreparation(nil, nil, nil, preparedRPGArchiveOptions(record)),
		NewImportPreparation(nil, nil, store, ImportPreparationOptions{}),
	} {
		if _, _, _, err := service.PrepareRPGMakerProject(t.Context(), "DIRECTORY", nil, detector.VirtualCoreID); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("unconfigured detector error=%v", err)
		}
	}
	service := NewImportPreparation(nil, nil, store, preparedRPGArchiveOptions(record))
	if _, _, _, err := service.PrepareRPGMakerProject(t.Context(), "UNSUPPORTED", nil, detector.VirtualCoreID); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("unsupported input error=%v", err)
	}
	if record.calls != 0 {
		t.Fatalf("invalid input called detector %d times", record.calls)
	}
}

func checkPreparedRPGPort(t *testing.T, sourceType string, fail bool) {
	t.Helper()
	ctx := t.Context()
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inputs := preparedRPGProbeInputs(t, store, sourceType)
	record := &preparedRPGProbeRecorder{t: t, wantContext: ctx}
	if fail {
		record.fail = errors.New("public detector failure")
	}
	service := NewImportPreparation(nil, nil, store, preparedRPGArchiveOptions(record))
	dispositions, groups, archives, err := service.PrepareRPGMakerProject(ctx, sourceType, inputs, detector.VirtualCoreID)
	if record.calls != 1 || record.coreID != detector.VirtualCoreID {
		t.Fatalf("calls=%d core=%q", record.calls, record.coreID)
	}
	facts := make([]detector.File, 0, len(record.files))
	for _, file := range record.files {
		facts = append(facts, file.File)
	}
	want := []detector.File{{Path: "Map0001.lmu", Size: 3}, {Path: "RPG_RT.ldb", Size: 8}, {Path: "RPG_RT.lmt", Size: 4}, {Path: "Save01.lsd", Size: 4}}
	if !reflect.DeepEqual(facts, want) {
		t.Fatalf("probe facts=%#v, want %#v", facts, want)
	}
	checkPreparedRPGOutcome(t, sourceType, record.fail, dispositions, groups, archives, err)
	if sourceType == "FILES" {
		for _, file := range record.files {
			if _, statErr := os.Stat(file.SourcePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("candidate remains after owner finished: path=%q error=%v", file.File.Path, statErr)
			}
		}
	}
}

func checkPreparedRPGOutcome(
	t *testing.T, sourceType string, failure error,
	dispositions []model.PreparedDisposition, groups []model.PreparedGroup, archives []model.PreparedArchive,
	err error,
) {
	t.Helper()
	if failure != nil {
		checkPreparedRPGFailure(t, sourceType, failure, err)
		if len(dispositions) != 0 || len(groups) != 0 || len(archives) != 0 {
			t.Fatal("failure returned prepared results")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].RPGProfile.ExpectedGeneration != detector.RPG2000 ||
		len(groups[0].Sources) != 3 {
		t.Fatalf("prepared groups=%#v", groups)
	}
	if sourceType == "FILES" {
		if len(archives) != 1 || len(archives[0].Materialized) != 3 {
			t.Fatalf("materialized archives=%#v", archives)
		}
	}
}

func putPreparedRPGSource(t *testing.T, store *blobstore.Store, logical, contents string) model.ImportFile {
	t.Helper()
	blob, err := store.Put(strings.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	return model.ImportFile{Path: logical, SHA256: blob.SHA256, Size: blob.Size}
}

func preparedRPGArchiveOptions(record *preparedRPGProbeRecorder) ImportPreparationOptions {
	reporter := cleanupadapter.NewReporter(slog.Default())
	archives := archiveadapter.New(reporter)
	return ImportPreparationOptions{
		RPGMakerDetector: record, ProjectArchives: archives, ArchiveInspector: archives, Diagnostics: reporter,
	}
}

func checkPreparedRPGFailure(t *testing.T, sourceType string, failure, err error) {
	t.Helper()
	if !errors.Is(err, failure) {
		t.Fatalf("detection lost original cause: %v", err)
	}
	part := "directory"
	if sourceType == "FILES" {
		part = "archive"
	}
	if err.Error() != "detect RPG Maker "+part+": public detector failure" {
		t.Fatalf("detection error=%q", err)
	}
}
