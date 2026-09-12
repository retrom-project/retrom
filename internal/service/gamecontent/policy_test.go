package gamecontent

import (
	"errors"
	"testing"

	"retrom/internal/contentcapability"
	"retrom/internal/multidisc"
)

func TestReplacementUploadModeAndConsumptionRules(t *testing.T) {
	for _, test := range []struct {
		name, mode, platform string
		upload               Upload
		valid                bool
	}{
		{"single", "STANDARD", "gba", Upload{State: "COMPLETE", FileCount: 1}, true},
		{"incomplete", "STANDARD", "gba", Upload{State: "UPLOADING", FileCount: 1}, false},
		{"consumed", "STANDARD", "gba", Upload{State: "COMPLETE", FileCount: 1, Consumptions: 1}, false},
		{"multiple single", "STANDARD", "gba", Upload{State: "COMPLETE", FileCount: 2}, false},
		{"DOS companions", "STANDARD", "dos", Upload{State: "COMPLETE", FileCount: 2}, true},
		{"disc requires directory", "MULTI_DISC", "psx", Upload{State: "COMPLETE", SourceType: "FILES", FileCount: 3}, false},
		{"disc directory", "MULTI_DISC", "psx", Upload{State: "COMPLETE", SourceType: "DIRECTORY", FileCount: 3}, true},
		{"RPG directory", "RPG_MAKER_PROJECT", "rpgmaker", Upload{State: "COMPLETE", SourceType: "DIRECTORY", FileCount: 2}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateUpload(test.upload, test.mode, test.platform)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid upload accepted: %v", err)
			}
		})
	}
}

func TestReplacementIdentityUsesDiscOrderAndContentBytes(t *testing.T) {
	prepared := PreparedReplacement{ContentKind: multidisc.ContentKind, OrderedDiscSHA256: []string{"first", "second"}, Files: []ReplacementFile{{Role: "PLAYLIST_SOURCE", SHA256: "renamed-playlist"}}}
	identity := preparedContentIdentity(prepared)
	if len(identity) != 2 || identity[0] != (IdentityFile{"DISC", "first"}) || identity[1] != (IdentityFile{"DISC", "second"}) {
		t.Fatalf("disc identity: %+v", identity)
	}
	prepared.ContentKind = "SINGLE_FILE"
	identity = preparedContentIdentity(prepared)
	if len(identity) != 1 || identity[0].SHA256 != "renamed-playlist" {
		t.Fatalf("file identity: %+v", identity)
	}
}

func TestReplacementBindingDetectsBusinessConfigurationChanges(t *testing.T) {
	binding := Binding{ManifestDigest: "manifest", InstanceID: "instance", PlatformID: "gba", CoreID: "core", ProviderID: "provider", TargetID: "target", VariantID: "variant", Version: 3, PlatformVersion: 2}
	snapshot := JobSnapshot{BaseManifestDigest: "manifest", PlatformInstanceID: "instance", PlatformID: "gba", CoreID: "core", ProviderID: "provider", TargetID: "target", VariantID: "variant", GameVersion: 3, PlatformInstanceVersion: 2, ContentPolicy: contentcapability.Policy{}}
	if !replacementBindingMatchesSnapshot(binding, snapshot) {
		t.Fatal("matching binding rejected")
	}
	for _, change := range []func(*Binding){
		func(value *Binding) { value.Version++ }, func(value *Binding) { value.PlatformVersion++ },
		func(value *Binding) { value.TargetID = "changed" }, func(value *Binding) { value.ManifestDigest = "changed" },
		func(value *Binding) { value.RPGRequirementsSHA256 = "changed" },
	} {
		current := binding
		change(&current)
		if replacementBindingMatchesSnapshot(current, snapshot) {
			t.Fatalf("changed binding accepted: %+v", current)
		}
	}
}
