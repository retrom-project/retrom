package launch

import (
	"errors"
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
		change func(*PreviewRestore)
	}{
		{"actor", func(r *PreviewRestore) { r.ActorID = "other" }},
		{"item", func(r *PreviewRestore) { r.ItemID = "other" }},
		{"source", func(r *PreviewRestore) { r.SnapshotID = "other" }},
		{"provider", func(r *PreviewRestore) { r.ProviderID = "other" }},
		{"target", func(r *PreviewRestore) { r.TargetID = "other" }},
		{"created", func(r *PreviewRestore) { r.State = "CREATED" }},
		{"revoked", func(r *PreviewRestore) { r.State = "REVOKED" }},
		{"hard boundary", func(r *PreviewRestore) { r.HardExpiresAtMS = 1000 }},
		{"blob", func(r *PreviewRestore) { r.ContentBlobID = "other" }},
		{"name", func(r *PreviewRestore) { r.ContentName = "other" }},
		{"format", func(r *PreviewRestore) { r.ContentFormat = "other" }},
		{"dependencies", func(r *PreviewRestore) { r.DependencySnapshot = "other" }},
		{"empty payload", func(r *PreviewRestore) { r.BlobID = "" }},
		{"empty bytes", func(r *PreviewRestore) { r.SizeBytes = 0 }},
		{"over limit", func(r *PreviewRestore) { r.SizeBytes = 101 }},
		{"unsupported", func(r *PreviewRestore) { r.ReadFormats = []string{"another"} }},
		{"extra file", func(r *PreviewRestore) {
			r.Files = []PreviewFile{{Role: "PARENT", BlobID: "other", LogicalName: "parent.zip"}}
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
			if !errors.Is(err, ErrSaveIncompatible) || result.PreviewID != "" || len(repository.writes) != 0 {
				t.Fatalf("incompatible restore wrote: id=%q error=%v", result.PreviewID, err)
			}
		})
	}
}

func TestPreviewRestoreFilesCompareEveryFrozenDimension(t *testing.T) {
	t.Parallel()
	path := "/bios.bin"
	file := PreviewFile{Role: "EXTERNAL_FILE", LogicalName: "bios.bin", BlobID: "blob", VirtualPath: &path, SortOrder: 1}
	cases := []struct {
		name   string
		change func(*PreviewFile)
	}{
		{"role", func(f *PreviewFile) { f.Role = "PARENT" }},
		{"name", func(f *PreviewFile) { f.LogicalName = "different.bin" }},
		{"blob", func(f *PreviewFile) { f.BlobID = "changed" }},
		{"path", func(f *PreviewFile) { p := "/new.bin"; f.VirtualPath = &p }},
		{"order", func(f *PreviewFile) { f.SortOrder = 2 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := file
			test.change(&changed)
			if samePreviewFiles([]PreviewFile{file}, []PreviewFile{changed}) {
				t.Fatal("changed frozen file accepted")
			}
		})
	}
	if !samePreviewFiles([]PreviewFile{file, {BlobID: "other"}}, []PreviewFile{{BlobID: "other"}, file}) {
		t.Fatal("read order changed file identity")
	}
	if samePreviewFiles([]PreviewFile{file, file}, []PreviewFile{file, {BlobID: "other"}}) {
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
	if !errors.Is(err, ErrSaveIncompatible) || result.PreviewID != "" || calls != 2 || len(repository.writes) != 0 {
		t.Fatalf("restore crossed hard expiry: id=%q calls=%d error=%v", result.PreviewID, calls, err)
	}
}
