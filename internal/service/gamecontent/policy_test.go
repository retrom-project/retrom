package gamecontent

import (
	"errors"
	model "retrom/internal/model/gamecontent"
	"testing"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/multidisc"
)

func TestReplacementUploadModeAndConsumptionRules(t *testing.T) {
	for _, test := range []struct {
		name, mode, platform string
		upload               model.Upload
		valid                bool
	}{
		{"single", "STANDARD", "gba", model.Upload{State: "COMPLETE", FileCount: 1}, true},
		{"incomplete", "STANDARD", "gba", model.Upload{State: "UPLOADING", FileCount: 1}, false},
		{"consumed", "STANDARD", "gba", model.Upload{State: "COMPLETE", FileCount: 1, Consumptions: 1}, false},
		{"multiple single", "STANDARD", "gba", model.Upload{State: "COMPLETE", FileCount: 2}, false},
		{"DOS companions", "STANDARD", "dos", model.Upload{State: "COMPLETE", FileCount: 2}, true},
		{"disc requires directory", "MULTI_DISC", "psx", model.Upload{State: "COMPLETE", SourceType: "FILES", FileCount: 3}, false},
		{"disc directory", "MULTI_DISC", "psx", model.Upload{State: "COMPLETE", SourceType: "DIRECTORY", FileCount: 3}, true},
		{"RPG directory", "RPG_MAKER_PROJECT", "rpgmaker", model.Upload{State: "COMPLETE", SourceType: "DIRECTORY", FileCount: 2}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateUpload(test.upload, test.mode, test.platform)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("invalid upload accepted: %v", err)
			}
		})
	}
}

func TestReplacementIdentityUsesDiscOrderAndContentBytes(t *testing.T) {
	prepared := model.PreparedReplacement{ContentKind: multidisc.ContentKind, OrderedDiscSHA256: []string{"first", "second"}, Files: []model.ReplacementFile{{Role: "PLAYLIST_SOURCE", SHA256: "renamed-playlist"}}}
	identity := preparedContentIdentity(prepared)
	if len(identity) != 2 || identity[0] != (model.IdentityFile{Role: "DISC", SHA256: "first"}) || identity[1] != (model.IdentityFile{Role: "DISC", SHA256: "second"}) {
		t.Fatalf("disc identity: %+v", identity)
	}
	prepared.ContentKind = "SINGLE_FILE"
	identity = preparedContentIdentity(prepared)
	if len(identity) != 1 || identity[0].SHA256 != "renamed-playlist" {
		t.Fatalf("file identity: %+v", identity)
	}
}

func TestReplacementBindingDetectsBusinessConfigurationChanges(t *testing.T) {
	binding := model.Binding{ManifestDigest: "manifest", InstanceID: "instance", PlatformID: "gba", CoreID: "core", ProviderID: "provider", TargetID: "target", VariantID: "variant", Version: 3, PlatformVersion: 2}
	snapshot := model.JobSnapshot{BaseManifestDigest: "manifest", PlatformInstanceID: "instance", PlatformID: "gba", CoreID: "core", ProviderID: "provider", TargetID: "target", VariantID: "variant", GameVersion: 3, PlatformInstanceVersion: 2, ContentPolicy: contentcapability.Policy{}}
	if !replacementBindingMatchesSnapshot(binding, snapshot) {
		t.Fatal("matching binding rejected")
	}
	for _, change := range []func(*model.Binding){
		func(value *model.Binding) { value.Version++ }, func(value *model.Binding) { value.PlatformVersion++ },
		func(value *model.Binding) { value.TargetID = "changed" }, func(value *model.Binding) { value.ManifestDigest = "changed" },
		func(value *model.Binding) { value.RPGRequirementsSHA256 = "changed" },
	} {
		current := binding
		change(&current)
		if replacementBindingMatchesSnapshot(current, snapshot) {
			t.Fatalf("changed binding accepted: %+v", current)
		}
	}
}
