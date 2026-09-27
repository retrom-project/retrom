package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/filestore"
)

type reconfigurationRepositoryStub struct {
	source    ReconfigurationSource
	found     bool
	clone     ReconfigurationClone
	removed   string
	cloneErr  error
	removeErr error
	sourceErr error
}

func (stub *reconfigurationRepositoryStub) Source(
	context.Context, string, int64,
) (ReconfigurationSource, bool, error) {
	return stub.source, stub.found, stub.sourceErr
}

func (stub *reconfigurationRepositoryStub) Clone(_ context.Context, clone ReconfigurationClone) error {
	stub.clone = clone
	return stub.cloneErr
}

func (stub *reconfigurationRepositoryStub) RemoveUnused(_ context.Context, uploadID string, _ int64) error {
	stub.removed = uploadID
	return stub.removeErr
}

func TestReconfigurationsRejectsMissingSourceFiles(t *testing.T) {
	stub := &reconfigurationRepositoryStub{found: true}
	service := NewReconfigurations(stub, func(context.Context, ImportRequest,
		ImportCreationOptions,
	) (ImportCreationResult, error) {
		t.Fatal("create should not be called")
		return ImportCreationResult{}, nil
	}, reconfigurationCopy(1), nil, time.Now)
	if _, err := service.Reconfigure(context.Background(), ReconfigurationRequest{
		SourceImportJobID: "source", ExpectedVersion: 1,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

func TestReconfigurationsClonesAndCreatesReplacement(t *testing.T) {
	files := []PreparedReusableUploadFile{{ID: "file", Path: "rejected.rom", FileRecord: "blob", Size: 7}}
	stub := &reconfigurationRepositoryStub{found: true, source: ReconfigurationSource{
		SourceType: "FILES", Files: files,
	}}
	var gotRequest ImportRequest
	var gotOptions ImportCreationOptions
	service := NewReconfigurations(stub, func(_ context.Context, request ImportRequest,
		options ImportCreationOptions,
	) (ImportCreationResult, error) {
		gotRequest, gotOptions = request, options
		return ImportCreationResult{Created: ServerCreated{ImportJobID: "replacement"}}, nil
	}, reconfigurationCopy(7), nil, func() time.Time { return time.UnixMilli(1234) })

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
	assertReconfiguredCopy(t, files, stub.clone)
}

func assertReconfiguredCopy(t *testing.T, files []PreparedReusableUploadFile, clone ReconfigurationClone) {
	t.Helper()
	if files[0].FileRecord != "blob" || clone.Files[0].FileRecord != "independent-copy" || len(clone.Metadata) != 1 {
		t.Fatalf("replacement did not copy independently or mutated the source: source=%#v clone=%#v", files, clone)
	}
}

func TestReconfigurationsCleansCloneAfterCreationFailure(t *testing.T) {
	stub := &reconfigurationRepositoryStub{found: true, source: ReconfigurationSource{
		SourceType: "FILES", Files: []PreparedReusableUploadFile{{ID: "file", Path: "a", FileRecord: "blob", Size: 1}},
	}}
	wantErr := errors.New("create failed")
	service := NewReconfigurations(stub, func(context.Context, ImportRequest,
		ImportCreationOptions,
	) (ImportCreationResult, error) {
		return ImportCreationResult{}, wantErr
	}, reconfigurationCopy(1), nil, time.Now)
	_, err := service.Reconfigure(context.Background(),
		ReconfigurationRequest{SourceImportJobID: "source", ExpectedVersion: 1})
	if !errors.Is(err, wantErr) || stub.removed == "" {
		t.Fatalf("error/removed = %v/%q", err, stub.removed)
	}
}

func reconfigurationCopy(size int64) func(context.Context, string, string, string) (filestore.Metadata, error) {
	return func(context.Context, string, string, string) (filestore.Metadata, error) {
		return filestore.Metadata{Record: "independent-copy", Size: size}, nil
	}
}

func TestReconfigurationRejectsFailedOrInvalidCopiesBeforeClone(t *testing.T) {
	cause := errors.New("source disappeared")
	for _, test := range []struct {
		name     string
		metadata filestore.Metadata
		copyErr  error
		wantErr  error
	}{
		{name: "read failure", copyErr: cause, wantErr: cause},
		{name: "missing identity", wantErr: ErrVersionConflict},
		{name: "reused identity", metadata: filestore.Metadata{Record: "original", Size: 1}, wantErr: ErrVersionConflict},
		{name: "changed size", metadata: filestore.Metadata{Record: "new", Size: 2}, wantErr: ErrVersionConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &reconfigurationRepositoryStub{found: true, source: ReconfigurationSource{
				SourceType: "FILES", Files: []PreparedReusableUploadFile{{FileRecord: "original", Size: 1}},
			}}
			service := NewReconfigurations(stub,
				func(context.Context, ImportRequest, ImportCreationOptions) (ImportCreationResult, error) {
					t.Fatal("invalid copies must not create an import")
					return ImportCreationResult{}, nil
				}, func(context.Context, string, string, string) (filestore.Metadata, error) {
					return test.metadata, test.copyErr
				}, nil, time.Now)
			_, err := service.Reconfigure(t.Context(), ReconfigurationRequest{SourceImportJobID: "source", ExpectedVersion: 1})
			if !errors.Is(err, test.wantErr) || stub.clone.UploadID != "" {
				t.Fatalf("invalid copy reached transaction: error=%v clone=%#v", err, stub.clone)
			}
		})
	}
}
