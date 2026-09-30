package libraryimport

import (
	"errors"
	"testing"
	"time"

	launch "retrom/internal/service/launch"
)

func TestReviewPreviewsFreezesActiveRestoreWithoutClosingOriginal(t *testing.T) {
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
	if plan.RestoreFileRecord == nil || *plan.RestoreFileRecord != "copy-saved-B" ||
		plan.RestoreFormat == nil || *plan.RestoreFormat != "checkpoint-v1" {
		t.Fatal("checkpoint was not frozen")
	}
	repository.restore.FileRecord = "saved-C"
	if *plan.RestoreFileRecord != "copy-saved-B" {
		t.Fatal("later source save changed the prepared restore")
	}
}

func TestReviewPreviewsRejectsIncompatibleRestore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*launch.PreviewRestore)
	}{
		{"actor", func(r *launch.PreviewRestore) { r.ActorID = "other" }},
		{"item", func(r *launch.PreviewRestore) { r.ItemID = "other" }},
		{"source", func(r *launch.PreviewRestore) { r.SnapshotID = "other" }},
		{"provider", func(r *launch.PreviewRestore) { r.ProviderID = "other" }},
		{"target", func(r *launch.PreviewRestore) { r.TargetID = "other" }},
		{"created", func(r *launch.PreviewRestore) { r.State = "CREATED" }},
		{"revoked", func(r *launch.PreviewRestore) { r.State = "REVOKED" }},
		{"hard boundary", func(r *launch.PreviewRestore) { r.HardExpiresAtMS = 1000 }},
		{"blob", func(r *launch.PreviewRestore) { r.ContentFileRecord = "other" }},
		{"name", func(r *launch.PreviewRestore) { r.ContentName = "other" }},
		{"format", func(r *launch.PreviewRestore) { r.ContentFormat = "other" }},
		{"dependencies", func(r *launch.PreviewRestore) { r.DependencySnapshot = "other" }},
		{"empty payload", func(r *launch.PreviewRestore) { r.FileRecord = "" }},
		{"empty bytes", func(r *launch.PreviewRestore) { r.SizeBytes = 0 }},
		{"over limit", func(r *launch.PreviewRestore) { r.SizeBytes = 101 }},
		{"unsupported", func(r *launch.PreviewRestore) { r.ReadFormats = []string{"another"} }},
		{"extra file", func(r *launch.PreviewRestore) {
			r.Files = []launch.PreviewFile{{Role: "PARENT", FileRecord: "other", LogicalName: "parent.zip"}}
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
			if !errors.Is(err, launch.ErrSaveIncompatible) || result.PreviewID != "" || len(repository.writes) != 0 {
				t.Fatalf("incompatible restore wrote: id=%q error=%v", result.PreviewID, err)
			}
		})
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
	if !errors.Is(err, launch.ErrSaveIncompatible) || result.PreviewID != "" || calls != 2 || len(repository.writes) != 0 {
		t.Fatalf("restore crossed hard expiry: id=%q calls=%d error=%v", result.PreviewID, calls, err)
	}
}
