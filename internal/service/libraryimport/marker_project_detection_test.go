package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	butterscotchdetector "retrom/internal/capability/engine/butterscotch/detector"
	nxenginedetector "retrom/internal/capability/engine/nxengine/detector"
	onsdetector "retrom/internal/capability/engine/ons/detector"
	model "retrom/internal/model/libraryimport"
)

type markerProbeRecorder struct {
	context context.Context
	calls   int
	files   []model.ProjectProbeFile
	failure error
}

func (record *markerProbeRecorder) capture(ctx context.Context, files []model.ProjectProbeFile) {
	record.context = ctx
	record.calls++
	record.files = append([]model.ProjectProbeFile(nil), files...)
}

type onsProbeFake struct{ record *markerProbeRecorder }

func (fake onsProbeFake) Detect(ctx context.Context, files []model.ProjectProbeFile) (onsdetector.Profile, error) {
	fake.record.capture(ctx, files)
	return onsdetector.Profile{MarkerPath: "0.txt", FontPath: "default.ttf", ScriptEncoding: "utf8"}, fake.record.failure
}

type butterscotchProbeFake struct{ record *markerProbeRecorder }

func (fake butterscotchProbeFake) Detect(ctx context.Context, files []model.ProjectProbeFile) (butterscotchdetector.Profile, error) {
	fake.record.capture(ctx, files)
	return butterscotchdetector.Profile{MarkerPath: "data.win", Compatibility: "GAMEMAKER_RUNTIME_TRIAL_REQUIRED"}, fake.record.failure
}

type nxengineProbeFake struct{ record *markerProbeRecorder }

func (fake nxengineProbeFake) Detect(ctx context.Context, files []model.ProjectProbeFile) (nxenginedetector.Profile, error) {
	fake.record.capture(ctx, files)
	return nxenginedetector.Profile{MarkerPath: "Doukutsu.exe", Compatibility: "NXENGINE_RUNTIME_TRIAL_REQUIRED"}, fake.record.failure
}

func TestMarkerProjectsUseExplicitDetectorPorts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		definition markerProjectDefinition
		marker     string
	}{
		{definition: onsMarkerProject, marker: "0.txt"},
		{definition: butterscotchMarkerProject, marker: "data.win"},
		{definition: nxengineMarkerProject, marker: "Doukutsu.exe"},
	} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fail=%v", test.definition.name, fail), func(t *testing.T) {
				t.Parallel()
				checkMarkerProjectPort(t, test.definition, test.marker, fail)
			})
		}
	}
}

func checkMarkerProjectPort(t *testing.T, definition markerProjectDefinition, marker string, fail bool) {
	t.Helper()
	ctx := t.Context()
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record := &markerProbeRecorder{}
	if fail {
		record.failure = errors.New("public probe failure")
	}
	service := NewImportPreparation(nil, nil, store, ImportPreparationOptions{
		ONSDetector: onsProbeFake{record}, ButterscotchDetector: butterscotchProbeFake{record},
		NXEngineDetector: nxengineProbeFake{record},
	})
	input := []model.ImportFile{
		{Path: "Wrapper/" + marker, SHA256: "marker-digest", Size: 17},
		{Path: "Wrapper/extra.txt", SHA256: "extra-digest", Size: 5},
		{Path: "Wrapper/.DS_Store", SHA256: "noise-digest", Size: 5},
	}
	_, group, err := service.prepareMarkerProjectDirectory(ctx, input, definition)
	want := []model.ProjectProbeFile{
		{LogicalPath: marker, DeclaredSize: 17, ResourcePath: store.Path("marker-digest")},
		{LogicalPath: "extra.txt", DeclaredSize: 5, ResourcePath: store.Path("extra-digest")},
	}
	if record.calls != 1 || record.context != ctx || !reflect.DeepEqual(record.files, want) {
		t.Fatalf("detector calls=%d ctx=%v files=%#v; want=%#v", record.calls, record.context == ctx, record.files, want)
	}
	if fail {
		if !errors.Is(err, record.failure) {
			t.Fatalf("lost detector cause: %v", err)
		}
		expected := "detect " + definition.name + " directory: detect " + definition.name + " project: public probe failure"
		if err.Error() != expected {
			t.Fatalf("error=%q want=%q", err, expected)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if group.DependencySnapshot == "" || group.ContentKind != definition.contentKind || len(group.Sources) != 2 {
		t.Fatalf("prepared marker group=%#v", group)
	}
}
