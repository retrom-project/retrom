package runtimelaunch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
)

func TestInstalledCandidateBuildsDeterministicEnvelopeForAll47Bindings(t *testing.T) {
	root := os.Getenv("RETROM_PROVIDER_TEST_ROOT")
	if root == "" {
		t.Skip("RETROM_PROVIDER_TEST_ROOT is required for the cross-repository candidate matrix")
	}
	activeContents, err := os.ReadFile(filepath.Join(root, "active.json"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := runtimebundle.ParseActiveDescriptor(activeContents)
	if err != nil {
		t.Fatal(err)
	}
	manifests := loadCandidateManifests(t, root, active)
	builder, err := NewBuilder(active, manifests)
	if err != nil {
		t.Fatal(err)
	}
	catalogContents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(catalogContents)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Bindings) != 47 {
		t.Fatalf("binding count = %d", len(catalog.Bindings))
	}

	for _, binding := range catalog.Bindings {
		t.Run(binding.ProviderID+"/"+binding.TargetID, func(t *testing.T) {
			assertCandidateBinding(t, builder, manifests, binding)
		})
	}
}

func loadCandidateManifests(
	t *testing.T,
	root string,
	active runtimebundle.ActiveDescriptor,
) map[string]runtimebundle.Manifest {
	t.Helper()
	manifests := make(map[string]runtimebundle.Manifest, len(active.Providers))
	for _, provider := range active.Providers {
		directory := filepath.Join(root, "installed", filepath.FromSlash(provider.InstallationPath))
		manifestContents, readErr := os.ReadFile(filepath.Join(directory, "provider.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		manifest, parseErr := runtimebundle.ParseManifest(manifestContents)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		integrityContents, readErr := os.ReadFile(filepath.Join(directory, "integrity.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		integrity, parseErr := runtimebundle.ParseIntegrity(integrityContents)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		bound, bindErr := runtimebundle.BindTargetIntegrity(manifest, integrity.Files)
		if bindErr != nil {
			t.Fatal(bindErr)
		}
		manifests[provider.ProviderID] = bound
	}
	return manifests
}

func assertCandidateBinding(
	t *testing.T,
	builder *Builder,
	manifests map[string]runtimebundle.Manifest,
	binding runtimecatalog.Binding,
) {
	t.Helper()
	target := findManifestTarget(t, manifests[binding.ProviderID], binding.TargetID)
	input := Input{Binding: binding, Session: Session{
		ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
		Title: target.DisplayName, PlatformName: binding.PlatformIDs[0], ReturnTo: "/games/fixture",
	}, Resources: resourcesForTarget(target), TargetOptions: optionsForTarget(target.OptionsKind)}
	first, err := builder.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := builder.Build(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("Envelope bytes are not deterministic")
	}
	var envelope map[string]any
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	assertForbiddenKeysAbsent(t, envelope)
}

func findManifestTarget(t *testing.T, manifest runtimebundle.Manifest, targetID string) runtimebundle.Target {
	t.Helper()
	for _, target := range manifest.Targets {
		if target.ID == targetID {
			return target
		}
	}
	t.Fatalf("target %q missing", targetID)
	return runtimebundle.Target{}
}

func resourcesForTarget(target runtimebundle.Target) []map[string]any {
	result := make([]map[string]any, 0, len(target.Inputs))
	for _, input := range target.Inputs {
		base := map[string]any{"kind": input.Kind, "ordinal": 0, "role": input.Role}
		switch input.Kind {
		case "ROM_BLOB_V1", "SEEKABLE_BLOB_V1", "PARENT_ARCHIVE_V1", "WASM4_CART_V1":
			base["rangeRequired"] = input.Kind == "SEEKABLE_BLOB_V1" || input.Kind == "PARENT_ARCHIVE_V1"
			base["sha256"], base["sizeBytes"], base["url"] = strings.Repeat("e", 64), 3, "/runtime/content/"+input.Role
		case "FILE_TREE_V1":
			base["contentDigest"], base["indexUrl"] = strings.Repeat("e", 64), "/runtime/content/"+input.Role+"/index"
		case "NATIVE_WEB_V1", "ISOLATED_WEB_V1":
			base["bootstrapTicket"], base["cleanupUrl"] = strings.Repeat("t", 48), "https://runtime.example.test/cleanup"
			base["contentDigest"], base["entryUrl"] = strings.Repeat("e", 64), "https://runtime.example.test/entry"
			base["origin"] = "https://runtime.example.test"
		case "BIOS_BUNDLE_V1", "EXTERNAL_FILE_SET_V1":
			base["files"] = []map[string]any{{
				"logicalName": "firmware", "sha256": strings.Repeat("e", 64),
				"sizeBytes": 3, "url": "/runtime/content/" + input.Role + "/firmware", "virtualPath": "firmware.bin",
			}}
		case "MULTI_DISC_V1":
			base["entries"] = []map[string]any{{
				"index": 0, "label": "Disc 1", "sha256": strings.Repeat("e", 64),
				"sizeBytes": 3, "url": "/runtime/content/" + input.Role + "/disc-1",
			}}
			base["initialDiscIndex"] = 0
		default:
			panic(fmt.Sprintf("unhandled resource kind %q", input.Kind))
		}
		result = append(result, base)
	}
	return result
}

func optionsForTarget(kind string) map[string]any {
	switch kind {
	case "NONE_V1":
		return map[string]any{"kind": kind}
	case "EMULATORJS_V1":
		return map[string]any{"kind": kind, "dosEntryPath": nil, "initialDiscIndex": nil}
	case "RPGMAKER_V1":
		return map[string]any{"kind": kind, "expectedRestorePosition": nil}
	case "ONS_PROJECT_V1":
		return map[string]any{"kind": kind, "scriptEncoding": "utf8"}
	case "KIRIKIRI_PROJECT_V1":
		return map[string]any{"kind": kind, "startupXp3Path": nil}
	default:
		panic(fmt.Sprintf("unhandled options kind %q", kind))
	}
}

func assertForbiddenKeysAbsent(t *testing.T, value any) {
	t.Helper()
	forbidden := map[string]bool{
		"routeKey": true, "runtimeFamily": true, "adapterKind": true, "adapterAbi": true,
		"saveAbi": true, "payloadKind": true, "nativeProfile": true, "resumeSlot": true, "coreArtifactId": true,
	}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if forbidden[key] {
					t.Fatalf("forbidden key %q in Envelope", key)
				}
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(value)
}
