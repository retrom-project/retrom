package gamecontent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"retrom/internal/capability/engine/rpgmaker/detector"
	model "retrom/internal/model/gamecontent"
)

type replacementRPGProbeRecorder struct {
	t           *testing.T
	wantContext context.Context
	calls       int
	coreID      string
	files       []model.RPGMakerBlobFile
	failure     error
}

func (record *replacementRPGProbeRecorder) DetectBlobs(
	ctx context.Context, coreID string, files []model.RPGMakerBlobFile,
) (detector.Profile, error) {
	record.t.Helper()
	if ctx != record.wantContext {
		record.t.Fatal("replacement detector lost context")
	}
	record.calls++
	record.coreID = coreID
	record.files = append([]model.RPGMakerBlobFile(nil), files...)
	if record.failure != nil {
		return detector.Profile{}, record.failure
	}
	return detector.Profile{SelectedCoreID: coreID, ExpectedGeneration: detector.RPG2000}, nil
}

func TestRPGMakerReplacementPortUsesBlobIdentities(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	record := &replacementRPGProbeRecorder{t: t, wantContext: ctx}
	service := New(nil, nil).WithRPGMakerDetector(record)
	files := []model.UploadedFile{
		{LogicalName: "Wrapper/RPG_RT.lmt", SHA256: "tree-digest", SizeBytes: 4},
		{LogicalName: "Wrapper/.DS_Store", SHA256: "noise-digest", SizeBytes: 5},
		{LogicalName: "Wrapper/RPG_RT.ldb", SHA256: "database-digest", SizeBytes: 8},
	}
	project, profile, err := service.detectRPGMakerReplacement(ctx, files)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.RPGMakerBlobFile{
		{File: detector.File{Path: "RPG_RT.ldb", Size: 8}, SHA256: "database-digest"},
		{File: detector.File{Path: "RPG_RT.lmt", Size: 4}, SHA256: "tree-digest"},
	}
	if record.calls != 1 || record.coreID != detector.VirtualCoreID || !reflect.DeepEqual(record.files, want) {
		t.Fatalf("replacement detector calls=%d core=%q facts=%#v", record.calls, record.coreID, record.files)
	}
	if project.Root != "." || len(project.Files) != 2 || profile.ExpectedGeneration != detector.RPG2000 {
		t.Fatalf("replacement project=%#v profile=%#v", project, profile)
	}
}

func TestRPGMakerReplacementPortKeepsValidationMapping(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cause := &detector.Error{Code: detector.CodeProjectNotFound}
	record := &replacementRPGProbeRecorder{t: t, wantContext: ctx, failure: cause}
	service := New(nil, nil).WithRPGMakerDetector(record)
	files := []model.UploadedFile{{LogicalName: "RPG_RT.ldb", SHA256: "database-digest", SizeBytes: 8}}
	_, _, err := service.detectRPGMakerReplacement(ctx, files)
	var validation *replacementValidationError
	if !errors.As(err, &validation) || validation.code != string(cause.Code) || errors.Is(err, cause) {
		t.Fatalf("replacement must preserve code-only mapping: %v", err)
	}
	record.failure = errors.New("unclassified public error")
	_, _, err = service.detectRPGMakerReplacement(ctx, files)
	if !errors.As(err, &validation) || validation.code != "RPG_REPLACEMENT_INPUT_INVALID" {
		t.Fatalf("unexpected unclassified mapping: %v", err)
	}
	record.calls = 0
	if _, _, err := service.detectRPGMakerReplacement(ctx, nil); err == nil || record.calls != 0 {
		t.Fatalf("invalid project invoked detector: calls=%d error=%v", record.calls, err)
	}
}
