package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/contentcapability"
)

type admissionMemory struct {
	upload                             model.ImportUpload
	target                             model.ImportTarget
	files                              []model.ImportFile
	bindings                           []model.ImportBinding
	readError, writeError, commitError error
	change                             model.ImportAdmissionChange
	events                             []string
}

func admissionServiceFixture() (*ImportAdmissions, *admissionMemory, model.ImportRequest) {
	memory := &admissionMemory{
		upload:   model.ImportUpload{ID: "upload", Purpose: "GENERAL", SourceType: "FILES", State: "COMPLETE", Version: 3, FileCount: 1, ManifestDigest: "manifest"},
		target:   model.ImportTarget{ID: "platform", PlatformID: "nes", DefaultCoreID: "core", Version: 2},
		files:    []model.ImportFile{{ID: "file", Path: "game.nes", BlobID: "blob", SHA256: "digest", Size: 32}},
		bindings: []model.ImportBinding{{BindingID: "binding", CoreID: "core", ProviderID: "provider", TargetID: "target", Policy: contentcapability.NewPolicy("SINGLE_FILE")}},
	}
	service := NewImportAdmissions(memory, memory, nil, model.ImportAdmissionOptions{Now: func() time.Time { return time.UnixMilli(500) }})
	return service, memory, model.ImportRequest{UploadID: "upload", TargetPlatformInstanceID: "platform", MetadataProvider: "NONE"}
}

func (memory *admissionMemory) WithAdmission(_ context.Context, work func(model.ImportAdmissionScope) error) error {
	memory.events = append(memory.events, "begin")
	if err := work(model.ImportAdmissionScope{Facts: memory, Writer: memory}); err != nil {
		return err
	}
	if memory.commitError != nil {
		return memory.commitError
	}
	memory.events = append(memory.events, "commit")
	return nil
}

func (memory *admissionMemory) Upload(context.Context, string) (model.ImportUpload, bool, error) {
	return memory.upload, memory.upload.ID != "", memory.readError
}

func (memory *admissionMemory) Target(context.Context, string) (model.ImportTarget, bool, error) {
	return memory.target, memory.target.ID != "", memory.readError
}

func (memory *admissionMemory) Files(context.Context, string) ([]model.ImportFile, error) {
	return memory.files, memory.readError
}

func (memory *admissionMemory) Bindings(context.Context, model.ImportBindingQuery) ([]model.ImportBinding, error) {
	return memory.bindings, memory.readError
}

func (memory *admissionMemory) Create(_ context.Context, change model.ImportAdmissionChange) error {
	memory.events = append(memory.events, "write")
	memory.change = change
	return memory.writeError
}

func (memory *admissionMemory) NotifyImportGroup(context.Context, string) {
	memory.events = append(memory.events, "notify")
}

func TestImportAdmissionFreezesInputAndNotifiesAfterCommit(t *testing.T) {
	t.Parallel()
	service, memory, request := admissionServiceFixture()
	result, err := service.Queue(t.Context(), request)
	if err != nil || result.ImportJobID == "" || result.JobID == "" || result.State != "QUEUED" || result.ItemCount != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(memory.events, []string{"begin", "write", "commit", "notify"}) {
		t.Fatalf("events=%v", memory.events)
	}
	change := memory.change
	if change.Upload.Version != 3 || change.Target.Version != 2 || change.NowMS != 500 || change.ContentMode != "STANDARD" {
		t.Fatalf("change=%+v", change)
	}
	assertAdmissionDocuments(t, change, result, request)
}

func assertAdmissionDocuments(t *testing.T, change model.ImportAdmissionChange, result model.ServerCreated, request model.ImportRequest) {
	t.Helper()
	var frozen model.QueuedImportRequest
	if err := json.Unmarshal([]byte(change.Documents.RequestJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.SchemaVersion != 1 || frozen.Request.UploadID != request.UploadID || frozen.Tags == nil || frozen.Request.TagIDs == nil {
		t.Fatalf("frozen=%+v", frozen)
	}
	var input admissionInputDocument
	if err := json.Unmarshal([]byte(change.Documents.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	if input.Inputs.ConfigDigest != change.Documents.RequestDigest || input.Scope.ID != result.ImportJobID || input.Inputs.UploadVersion != 3 {
		t.Fatalf("input=%+v", input)
	}
}

func TestImportAdmissionFailuresClearResultAndNeverNotify(t *testing.T) {
	t.Parallel()
	cause := errors.New("admission persistence unavailable")
	for _, stage := range []string{"read", "write", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			service, memory, request := admissionServiceFixture()
			switch stage {
			case "read":
				memory.readError = cause
			case "write":
				memory.writeError = cause
			case "commit":
				memory.commitError = cause
			}
			result, err := service.Queue(t.Context(), request)
			if !errors.Is(err, cause) || result != (model.ServerCreated{}) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			for _, event := range memory.events {
				if event == "notify" {
					t.Fatalf("notified after %s failure", stage)
				}
			}
		})
	}
}

func TestImportAdmissionIdentityFailurePrecedesAllWrites(t *testing.T) {
	t.Parallel()
	for failed := 1; failed <= 4; failed++ {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			t.Parallel()
			service, memory, request := admissionServiceFixture()
			cause := errors.New("identity unavailable")
			calls := 0
			service.newID = func() (string, error) {
				calls++
				if calls == failed {
					return "", cause
				}
				return fmt.Sprint(calls), nil
			}
			result, err := service.Queue(t.Context(), request)
			if !errors.Is(err, cause) || result != (model.ServerCreated{}) || calls != failed || !reflect.DeepEqual(memory.events, []string{"begin"}) {
				t.Fatalf("result=%+v err=%v calls=%d events=%v", result, err, calls, memory.events)
			}
		})
	}
}

func TestImportAdmissionRejectsUnavailableFacts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		prepare func(*admissionMemory)
	}{
		{"missing upload", func(m *admissionMemory) { m.upload.ID = "" }},
		{"incomplete upload", func(m *admissionMemory) { m.upload.State = "UPLOADING" }},
		{"missing target", func(m *admissionMemory) { m.target.ID = "" }},
		{"missing binding", func(m *admissionMemory) { m.bindings = nil }},
		{"missing policy", func(m *admissionMemory) { m.bindings[0].Policy = contentcapability.Policy{} }},
		{"missing file", func(m *admissionMemory) { m.files = nil }},
		{"partial manifest", func(m *admissionMemory) { m.upload.FileCount = 2 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service, memory, request := admissionServiceFixture()
			test.prepare(memory)
			result, err := service.Queue(t.Context(), request)
			if !errors.Is(err, model.ErrInvalid) || result != (model.ServerCreated{}) || !reflect.DeepEqual(memory.events, []string{"begin"}) {
				t.Fatalf("result=%+v err=%v events=%v", result, err, memory.events)
			}
		})
	}
}

func TestImportAdmissionDigestRejectsMalformedOrAlteredSnapshots(t *testing.T) {
	t.Parallel()
	service, memory, request := admissionServiceFixture()
	if _, err := service.Queue(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	document, digest := memory.change.Documents.RequestJSON, memory.change.Documents.RequestDigest
	if !MatchesImportDocumentDigest(document, digest) {
		t.Fatal("admission checksum cannot be read by worker")
	}
	if MatchesImportDocumentDigest(`{"invalid":`, digest) || MatchesImportDocumentDigest(`{"different":true}`, digest) {
		t.Fatal("worker accepted altered or malformed input")
	}
}

func TestImportAdmissionTypedAPI(t *testing.T) {
	t.Parallel()
	service := NewImportAdmissions(nil, nil, nil, model.ImportAdmissionOptions{})
	result, err := service.Queue(t.Context(), model.ImportRequest{})
	if !errors.Is(err, model.ErrInvalid) || result != (model.ServerCreated{}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
