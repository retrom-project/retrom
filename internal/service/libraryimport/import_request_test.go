package libraryimport

import (
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"

	"retrom/internal/capability/content/contentcapability"
)

func TestImportRequestOwnsTagsAndPreservesProtocolShape(t *testing.T) {
	t.Parallel()
	request := model.ImportRequest{UploadID: "upload", TargetPlatformInstanceID: "target", MetadataProvider: "NONE", TagIDs: []string{"tag"}}
	normalized, mode, err := NormalizeImportRequest(request)
	if err != nil || mode != "STANDARD" || normalized.ContentMode != "" {
		t.Fatalf("request=%+v mode=%s err=%v", normalized, mode, err)
	}
	request.TagIDs[0] = "mutated"
	if normalized.TagIDs[0] != "tag" {
		t.Fatal("request retained caller tag backing array")
	}
}

func TestImportRequestRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, request := range []model.ImportRequest{
		{},
		{UploadID: "upload", MetadataProvider: "NONE"},
		{UploadID: "upload", TargetPlatformInstanceID: "target", MetadataProvider: "UNKNOWN"},
		{UploadID: "upload", TargetPlatformInstanceID: "target", MetadataProvider: "NONE", ContentMode: "UNKNOWN"},
	} {
		if _, _, err := NormalizeImportRequest(request); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("request=%+v err=%v", request, err)
		}
	}
}

func TestRPGImportNormalizesGeneralArchiveBeforeAdmission(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"game.zip", "game.7Z"} {
		request, mode, err := NormalizeTargetImport(model.ImportRequest{MetadataProvider: "HASHEOUS"}, "STANDARD", "GENERAL", "FILES", []model.ImportFile{{Path: path}}, model.ImportTarget{PlatformID: "rpgmaker"})
		if err != nil || mode != contentcapability.ModeRPGMakerProject || request.ContentMode != mode || request.MetadataProvider != "NONE" {
			t.Fatalf("path=%s request=%+v mode=%s err=%v", path, request, mode, err)
		}
	}
	for _, files := range [][]model.ImportFile{nil, {{Path: "game.zip"}, {Path: "second.zip"}}, {{Path: "game.7z.001"}}, {{Path: "game.rom"}}} {
		_, _, err := NormalizeTargetImport(model.ImportRequest{}, "STANDARD", "GENERAL", "FILES", files, model.ImportTarget{PlatformID: "rpgmaker"})
		if !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("files=%v err=%v", files, err)
		}
	}
}

func TestImportAdmissionPreservesMetadataAndMultiDiscPolicies(t *testing.T) {
	t.Parallel()
	service, memory, request := admissionServiceFixture()
	request.MetadataProvider = "HASHEOUS"
	if _, err := service.Queue(t.Context(), request); !errors.Is(err, ErrMetadataScraperNotConfigured) {
		t.Fatalf("scraper err=%v", err)
	}
	request.MetadataProvider = "NONE"
	request.ContentMode = "MULTI_DISC"
	if _, err := service.Queue(t.Context(), request); !errors.Is(err, model.ErrMultiDiscModeUnavailable) {
		t.Fatalf("files multidisc err=%v", err)
	}
	memory.upload.SourceType = "DIRECTORY"
	if _, err := service.Queue(t.Context(), request); !errors.Is(err, model.ErrMultiDiscModeUnavailable) {
		t.Fatalf("disabled multidisc err=%v", err)
	}
	service.options.MultiDiscEnabled = true
	memory.target.PlatformID = "saturn"
	memory.bindings[0].Policy = contentcapability.NewPolicy("MULTI_DISC")
	if _, err := service.Queue(t.Context(), request); err != nil {
		t.Fatal(err)
	}
}

func TestImportAdmissionTagAssignmentRequiresActor(t *testing.T) {
	t.Parallel()
	service, memory, request := admissionServiceFixture()
	request.TagIDs = []string{"tag"}
	if _, err := service.Queue(t.Context(), request); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("tag authority err=%v", err)
	}
	if memory.change.ImportID != "" {
		t.Fatal("unauthenticated tags created import")
	}
}
