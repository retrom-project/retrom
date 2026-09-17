package launch

import (
	"errors"
	model "retrom/internal/model/launch"
	"testing"
	"time"
)

func TestPreviewCreatorFreezesActiveRestoreWithoutClosingOriginal(t *testing.T) {
	t.Parallel()
	creator, repository, _, request := previewFixture(t)
	original := "original"
	request.RestoreFromPreviewID = &original
	repository.restore = previewFixtureRestore(repository)
	result, err := creator.Create(t.Context(), request)
	if err != nil || result.PreviewID == original || len(repository.writes) != 1 {
		t.Fatalf("active source restore: id=%q error=%v", result.PreviewID, err)
	}
	plan := repository.writes[0]
	if plan.RestoreBlobID == nil || *plan.RestoreBlobID != "saved-B" || plan.RestoreFormat == nil || *plan.RestoreFormat != "checkpoint-v1" {
		t.Fatal("checkpoint was not frozen")
	}
	repository.restore.BlobID = "saved-C"
	if *plan.RestoreBlobID != "saved-B" {
		t.Fatal("later source save changed the prepared restore")
	}
}

func TestPreviewCreatorRejectsIncompatibleRestore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*model.PreviewRestore)
	}{
		{"actor", func(r *model.PreviewRestore) { r.ActorID = "other" }},
		{"item", func(r *model.PreviewRestore) { r.ItemID = "other" }},
		{"source", func(r *model.PreviewRestore) { r.SnapshotID = "other" }},
		{"provider", func(r *model.PreviewRestore) { r.ProviderID = "other" }},
		{"target", func(r *model.PreviewRestore) { r.TargetID = "other" }},
		{"created", func(r *model.PreviewRestore) { r.State = "CREATED" }},
		{"revoked", func(r *model.PreviewRestore) { r.State = "REVOKED" }},
		{"hard boundary", func(r *model.PreviewRestore) { r.HardExpiresAtMS = 1000 }},
		{"blob", func(r *model.PreviewRestore) { r.ContentBlobID = "other" }},
		{"name", func(r *model.PreviewRestore) { r.ContentName = "other" }},
		{"format", func(r *model.PreviewRestore) { r.ContentFormat = "other" }},
		{"dependencies", func(r *model.PreviewRestore) { r.DependencySnapshot = "other" }},
		{"empty payload", func(r *model.PreviewRestore) { r.BlobID = "" }},
		{"empty bytes", func(r *model.PreviewRestore) { r.SizeBytes = 0 }},
		{"over limit", func(r *model.PreviewRestore) { r.SizeBytes = 101 }},
		{"unsupported", func(r *model.PreviewRestore) { r.ReadFormats = []string{"another"} }},
		{"extra file", func(r *model.PreviewRestore) {
			r.Files = []model.PreviewFile{{Role: "PARENT", BlobID: "other", LogicalName: "parent.zip"}}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, _, request := previewFixture(t)
			original := "original"
			request.RestoreFromPreviewID = &original
			repository.restore = previewFixtureRestore(repository)
			test.change(&repository.restore)
			result, err := creator.Create(t.Context(), request)
			if !errors.Is(err, model.ErrSaveIncompatible) || result.PreviewID != "" || len(repository.writes) != 0 {
				t.Fatalf("incompatible restore wrote: id=%q error=%v", result.PreviewID, err)
			}
		})
	}
}

func TestPreviewRestoreFilesCompareEveryFrozenDimension(t *testing.T) {
	t.Parallel()
	path := "/bios.bin"
	file := model.PreviewFile{Role: "EXTERNAL_FILE", LogicalName: "bios.bin", BlobID: "blob", VirtualPath: &path, SortOrder: 1}
	cases := []struct {
		name   string
		change func(*model.PreviewFile)
	}{
		{"role", func(f *model.PreviewFile) { f.Role = "PARENT" }},
		{"name", func(f *model.PreviewFile) { f.LogicalName = "different.bin" }},
		{"blob", func(f *model.PreviewFile) { f.BlobID = "changed" }},
		{"path", func(f *model.PreviewFile) { p := "/new.bin"; f.VirtualPath = &p }},
		{"order", func(f *model.PreviewFile) { f.SortOrder = 2 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := file
			test.change(&changed)
			if samePreviewFiles([]model.PreviewFile{file}, []model.PreviewFile{changed}) {
				t.Fatal("changed frozen file accepted")
			}
		})
	}
	if !samePreviewFiles([]model.PreviewFile{file, {BlobID: "other"}}, []model.PreviewFile{{BlobID: "other"}, file}) {
		t.Fatal("read order changed file identity")
	}
	if samePreviewFiles([]model.PreviewFile{file, file}, []model.PreviewFile{file, {BlobID: "other"}}) {
		t.Fatal("duplicate erased a missing file")
	}
}

func TestPreviewRestoreUsesClockAtFinalAuthorization(t *testing.T) {
	t.Parallel()
	creator, repository, _, request := previewFixture(t)
	original := "original"
	request.RestoreFromPreviewID = &original
	repository.restore = previewFixtureRestore(repository)
	calls := 0
	creator.environment.Now = func() time.Time {
		calls++
		if calls == 1 {
			return time.UnixMilli(1000)
		}
		return time.UnixMilli(repository.restore.HardExpiresAtMS)
	}
	result, err := creator.Create(t.Context(), request)
	if !errors.Is(err, model.ErrSaveIncompatible) || result.PreviewID != "" || calls != 2 || len(repository.writes) != 0 {
		t.Fatalf("restore crossed hard expiry: id=%q calls=%d error=%v", result.PreviewID, calls, err)
	}
}
