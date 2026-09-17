package launch

import (
	"errors"
	model "retrom/internal/model/launch"
	"testing"

	"retrom/internal/capability/content/multidisc"
)

func TestProductRestoreErrorPriority(t *testing.T) {
	for _, test := range []struct {
		name                              string
		save, found, readable, compatible bool
		want                              error
	}{
		{name: "missing authorized save", found: true, readable: true, want: model.ErrBlocked},
		{name: "compatible selected core", save: true, found: true, compatible: true},
		{name: "no compatible core", save: true, found: true, want: model.ErrSaveIncompatible},
		{name: "different compatible core remains", save: true, found: true, readable: true, want: model.ErrBlocked},
		{name: "selected source unavailable", save: true, readable: true, want: model.ErrBlocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repository, _, command := productFixture(t)
			saveID := "save"
			command.Request.SaveStateID = &saveID
			repository.before.Found = test.found
			repository.before.SaveReadable = test.readable
			if test.save {
				repository.before.Save = &model.ProductSave{ID: saveID, Format: "native-v1"}
			}
			if test.compatible {
				repository.before.Source.ReadFormats = []string{"native-v1"}
			}
			repository.current = cloneProductSnapshot(t, repository.before)
			result, err := service.Create(t.Context(), command)
			if !errors.Is(err, test.want) || (test.want == nil) != (result.Created.LaunchID != "") {
				t.Fatalf("launch=%q error=%v want=%v", result.Created.LaunchID, err, test.want)
			}
		})
	}
}

func TestProductRestoreFreezesPayloadAndSelectedDisc(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*model.ProductSnapshot)
	}{
		{"deleted", func(snapshot *model.ProductSnapshot) { snapshot.Save = nil }},
		{"payload replaced", func(snapshot *model.ProductSnapshot) { snapshot.Save.PayloadID = "replacement" }},
		{"content changed", func(snapshot *model.ProductSnapshot) { snapshot.Save.Digest = "replacement" }},
		{"owner changed", func(snapshot *model.ProductSnapshot) { snapshot.Save.ProfileID = "other" }},
		{"disc changed", func(snapshot *model.ProductSnapshot) { disc := int64(1); snapshot.Save.DiscIndex = &disc }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repository, _, command := productFixture(t)
			saveID := "save"
			command.Request.SaveStateID = &saveID
			repository.before.Save = &model.ProductSave{ID: saveID, ProfileID: "profile", Format: "native-v1", PayloadID: "payload"}
			repository.before.Source.ReadFormats = []string{"native-v1"}
			repository.current = cloneProductSnapshot(t, repository.before)
			test.mutate(&repository.current)
			result, err := service.Create(t.Context(), command)
			if !errors.Is(err, model.ErrBlocked) || result.Created.LaunchID != "" || len(repository.writes) != 0 {
				t.Fatalf("changed restore returned launch=%q error=%v writes=%d", result.Created.LaunchID, err, len(repository.writes))
			}
		})
	}
}

func TestProductDiscRestoreBounds(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		hasSave    bool
		disc       *int64
		want       int64
		blocked    bool
	}{
		{name: "fresh multidisc", kind: multidisc.ContentKind},
		{name: "last valid disc", kind: multidisc.ContentKind, hasSave: true, disc: productDiscValue(1), want: 1},
		{name: "missing disc", kind: multidisc.ContentKind, hasSave: true, blocked: true},
		{name: "negative disc", kind: multidisc.ContentKind, hasSave: true, disc: productDiscValue(-1), blocked: true},
		{name: "past last disc", kind: multidisc.ContentKind, hasSave: true, disc: productDiscValue(2), blocked: true},
		{name: "single content with disc", kind: "SINGLE_FILE", hasSave: true, disc: productDiscValue(0), blocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := model.ProductSnapshot{Source: model.ProductSource{ContentKind: test.kind}}
			if test.hasSave {
				snapshot.Save = &model.ProductSave{DiscIndex: test.disc}
			}
			disc, err := productInitialDisc(snapshot, 2)
			if errors.Is(err, model.ErrBlocked) != test.blocked || disc != test.want {
				t.Fatalf("disc=%d error=%v", disc, err)
			}
		})
	}
}

func productDiscValue(value int64) *int64 { return &value }

func TestProductDOSRestoreUsesFrozenSafeEntry(t *testing.T) {
	entry := "GAME.EXE"
	command := model.ProductCreateCommand{Request: model.CreateRequest{DOSEntry: &entry}}
	savedEntry := "SAVED.EXE"
	snapshot := model.ProductSnapshot{Save: &model.ProductSave{DOSEntry: &savedEntry}}
	if _, err := selectedProductDOS(command, snapshot); !errors.Is(err, model.ErrDOSEntryMissing) {
		t.Fatalf("missing DOS entry: %v", err)
	}
	snapshot.DOS.Found = true
	if _, err := selectedProductDOS(command, snapshot); !errors.Is(err, model.ErrDOSEntryUnsafe) {
		t.Fatalf("unsafe DOS entry: %v", err)
	}
	snapshot.DOS.Safe = true
	choice, err := selectedProductDOS(command, snapshot)
	if err != nil || choice.Path == nil || *choice.Path != savedEntry {
		t.Fatalf("DOS choice=%+v error=%v", choice, err)
	}
}
