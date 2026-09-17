package libraryimport

import (
	"context"
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"
	"time"
)

type reconfigurationRepositoryStub struct {
	source    model.ReconfigurationSource
	found     bool
	clone     model.ReconfigurationClone
	removed   string
	cloneErr  error
	removeErr error
	sourceErr error
}

func (stub *reconfigurationRepositoryStub) Source(
	context.Context, string, int64,
) (model.ReconfigurationSource, bool, error) {
	return stub.source, stub.found, stub.sourceErr
}

func (stub *reconfigurationRepositoryStub) Clone(_ context.Context, clone model.ReconfigurationClone) error {
	stub.clone = clone
	return stub.cloneErr
}

func (stub *reconfigurationRepositoryStub) RemoveUnused(_ context.Context, uploadID string) error {
	stub.removed = uploadID
	return stub.removeErr
}

func TestReconfigurationsRejectsMissingSourceFiles(t *testing.T) {
	stub := &reconfigurationRepositoryStub{found: true}
	service := NewReconfigurations(stub, func(context.Context, model.ImportRequest, model.ImportCreationOptions) (model.ImportCreationResult, error) {
		t.Fatal("create should not be called")
		return model.ImportCreationResult{}, nil
	}, time.Now)
	if _, err := service.Reconfigure(context.Background(), ReconfigurationRequest{
		SourceImportJobID: "source", ExpectedVersion: 1,
	}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

func TestReconfigurationsClonesAndCreatesReplacement(t *testing.T) {
	files := []model.PreparedReusableUploadFile{{ID: "file", Path: "rejected.rom", BlobID: "blob", Size: 7}}
	stub := &reconfigurationRepositoryStub{found: true, source: model.ReconfigurationSource{
		SourceType: "FILES", Files: files,
	}}
	var gotRequest model.ImportRequest
	var gotOptions model.ImportCreationOptions
	service := NewReconfigurations(stub, func(_ context.Context, request model.ImportRequest, options model.ImportCreationOptions) (model.ImportCreationResult, error) {
		gotRequest, gotOptions = request, options
		return model.ImportCreationResult{Created: model.ServerCreated{ImportJobID: "replacement"}}, nil
	}, func() time.Time { return time.UnixMilli(1234) })

	result, err := service.Reconfigure(context.Background(), ReconfigurationRequest{
		SourceImportJobID: "source", ExpectedVersion: 3, TargetPlatformInstance: "target",
		MetadataProvider: "NONE", TagIDs: []string{"tag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created.ImportJobID != "replacement" || gotRequest.TargetPlatformInstanceID != "target" ||
		gotRequest.MetadataProvider != "NONE" || len(gotRequest.TagIDs) != 1 {
		t.Fatalf("result/request = %#v/%#v", result, gotRequest)
	}
	if gotOptions.Reconfiguration == nil || gotOptions.Reconfiguration.ImportID != "source" ||
		gotOptions.Reconfiguration.Version != 3 || len(gotOptions.Reconfiguration.FileIDs) != 1 {
		t.Fatalf("creation options = %#v", gotOptions)
	}
	if stub.clone.UploadID == "" || stub.clone.SourceType != "FILES" || stub.clone.NowMS != 1234 ||
		stub.clone.ManifestDigest == "" {
		t.Fatalf("clone = %#v", stub.clone)
	}
}

func TestReconfigurationsCleansCloneAfterCreationFailure(t *testing.T) {
	stub := &reconfigurationRepositoryStub{found: true, source: model.ReconfigurationSource{
		SourceType: "FILES", Files: []model.PreparedReusableUploadFile{{ID: "file", Path: "a", BlobID: "blob", Size: 1}},
	}}
	wantErr := errors.New("create failed")
	service := NewReconfigurations(stub, func(context.Context, model.ImportRequest, model.ImportCreationOptions) (model.ImportCreationResult, error) {
		return model.ImportCreationResult{}, wantErr
	}, time.Now)
	_, err := service.Reconfigure(context.Background(), ReconfigurationRequest{SourceImportJobID: "source", ExpectedVersion: 1})
	if !errors.Is(err, wantErr) || stub.removed == "" {
		t.Fatalf("error/removed = %v/%q", err, stub.removed)
	}
}
