package libraryimport

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/multidisc"
)

type multiDiscAttachmentSourceFixture struct {
	baseCalls, uploadCalls int
	base                   MultiDiscAttachmentBaseFiles
	upload                 MultiDiscAttachmentUploadFiles
	err                    error
}

func (fixture *multiDiscAttachmentSourceFixture) BaseFiles(
	context.Context, string,
) (MultiDiscAttachmentBaseFiles, error) {
	fixture.baseCalls++
	return fixture.base, fixture.err
}

func (fixture *multiDiscAttachmentSourceFixture) UploadFiles(
	context.Context, string,
) (MultiDiscAttachmentUploadFiles, error) {
	fixture.uploadCalls++
	return fixture.upload, fixture.err
}

func TestMultiDiscAttachmentSourcesDelegatesTypedReads(t *testing.T) {
	fixture := &multiDiscAttachmentSourceFixture{
		base:   MultiDiscAttachmentBaseFiles{Entries: []multidisc.Entry{{State: multidisc.EntryMissing}}},
		upload: MultiDiscAttachmentUploadFiles{State: "COMPLETE", SourceType: "FILES"},
	}
	service := NewMultiDiscAttachmentSources(fixture)

	base, err := service.BaseFiles(t.Context(), "snapshot")
	if err != nil || fixture.baseCalls != 1 || len(base.Entries) != 1 {
		t.Fatalf("base=%+v err=%v calls=%d", base, err, fixture.baseCalls)
	}
	upload, err := service.UploadFiles(t.Context(), "session")
	if err != nil || fixture.uploadCalls != 1 || upload.State != "COMPLETE" {
		t.Fatalf("upload=%+v err=%v calls=%d", upload, err, fixture.uploadCalls)
	}
}

func TestMultiDiscAttachmentSourcesRejectsInvalidIDsAndPreservesErrors(t *testing.T) {
	cause := errors.New("source unavailable")
	fixture := &multiDiscAttachmentSourceFixture{err: cause}
	service := NewMultiDiscAttachmentSources(fixture)

	if _, err := service.BaseFiles(t.Context(), ""); !errors.Is(err, ErrInvalid) || fixture.baseCalls != 0 {
		t.Fatalf("invalid base read err=%v calls=%d", err, fixture.baseCalls)
	}
	if _, err := service.UploadFiles(t.Context(), ""); !errors.Is(err, ErrInvalid) || fixture.uploadCalls != 0 {
		t.Fatalf("invalid upload read err=%v calls=%d", err, fixture.uploadCalls)
	}
	if _, err := service.BaseFiles(t.Context(), "snapshot"); !errors.Is(err, cause) {
		t.Fatalf("base error=%v", err)
	}
	if _, err := service.UploadFiles(t.Context(), "session"); !errors.Is(err, cause) {
		t.Fatalf("upload error=%v", err)
	}
}
