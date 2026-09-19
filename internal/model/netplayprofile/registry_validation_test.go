package netplayprofile

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"retrom/internal/capability/runtime/runtimecatalog"
)

func TestRegistryKeepsRejectionReasons(t *testing.T) {
	t.Parallel()
	raw, _, _ := compatibilityRegistry(t)
	text := string(raw)
	for _, test := range []struct {
		name, contents, reason string
		emptyBindings          bool
	}{
		{"malformed", "{", "schema", false},
		{"unknown field", strings.Replace(text, "{", "{\"unexpected\":1,", 1), "schema", false},
		{"trailing value", text + " {}", "trailing data", false},
		{"trailing invalid", text + " x", "trailing data", false},
		{"wrong schema version", strings.Replace(text, "\"schemaVersion\": 5", "\"schemaVersion\": 4", 1), "protocol", false},
		{"wrong protocol", strings.Replace(text, "retrom-netplay-v2", "retrom-netplay-v1", 1), "protocol", false},
		{"unsupported players", strings.Replace(text, "\"maxPlayers\":2", "\"maxPlayers\":1", 1), "profile", false},
		{"missing bindings", text, "profile", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bindings := fixtureBindings()
			if test.emptyBindings {
				bindings = nil
			}
			_, err := ParseRegistry([]byte(test.contents), bindings)
			if !errors.Is(err, ErrManifestInvalid) || err.Error() != "NETPLAY_MANIFEST_INVALID: "+test.reason {
				t.Fatalf("error = %v, want sentinel / %s", err, test.reason)
			}
		})
	}
}

func TestRegistryPreservesOrderedBindingAndProfileValidation(t *testing.T) {
	t.Parallel()
	_, registry, _ := compatibilityRegistry(t)
	manifest := registry.Manifest
	manifest.Profiles = slices.Clone(manifest.Profiles[:1])
	selected := manifest.Profiles[0]
	bindings := fixtureBindings()
	index := slices.IndexFunc(bindings, func(binding runtimecatalog.Binding) bool {
		return binding.ProviderID == selected.ProviderID && binding.TargetID == selected.TargetID && binding.CoreID == selected.CoreID
	})
	if index < 0 {
		t.Fatal("fixture binding missing")
	}
	correct := bindings[index]
	wrongPlatform := correct
	wrongPlatform.PlatformIDs = []string{"other"}
	disabled := correct
	disabled.LaunchPolicy = "DISABLED"
	for _, test := range []struct {
		name     string
		bindings []runtimecatalog.Binding
		accepted bool
	}{
		{"exact", []runtimecatalog.Binding{correct}, true},
		{"disabled skipped", []runtimecatalog.Binding{disabled, correct}, true},
		{"first platform mismatch", []runtimecatalog.Binding{wrongPlatform, correct}, false},
		{"disabled only", []runtimecatalog.Binding{disabled}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ParseRegistry(raw, test.bindings)
			if (err == nil) != test.accepted {
				t.Fatalf("acceptance = %v, want %t", err, test.accepted)
			}
		})
	}
	manifest.Profiles[0].ID = "lower space/é"
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRegistry(raw, []runtimecatalog.Binding{correct}); err != nil {
		t.Fatalf("existing permissive lowercase profile ID changed: %v", err)
	}
	manifest.Profiles = append(manifest.Profiles, manifest.Profiles[0])
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRegistry(raw, []runtimecatalog.Binding{correct}); !errors.Is(err, ErrManifestInvalid) || err.Error() != "NETPLAY_MANIFEST_INVALID: duplicate profile" {
		t.Fatalf("duplicate reason = %v", err)
	}
}

func TestRegistryProfilesKeepManifestOrder(t *testing.T) {
	t.Parallel()
	_, registry, _ := compatibilityRegistry(t)
	original := registry.Manifest.Profiles[0].ID
	sorted := registry.Profiles()
	if !slices.IsSortedFunc(sorted, func(a, b ManifestProfile) int { return strings.Compare(a.ID, b.ID) }) {
		t.Fatal("profiles are not sorted by ID")
	}
	sorted[0].ID = "replacement"
	if registry.Manifest.Profiles[0].ID != original {
		t.Fatal("Profiles mutated manifest order")
	}
	var absent *Registry
	if absent.SupportsPlatformTarget("nes", "fceumm", "emulatorjs", "fceumm") {
		t.Fatal("nil registry supports a target")
	}
}
