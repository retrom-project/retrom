package runtimeprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
	"retrom/internal/store"
)

func TestLoadInstallationValidatesAndReconcilesBeforeServing(t *testing.T) {
	root := t.TempDir()
	paths := writeInstallationFixture(t, root)
	installation, err := LoadInstallation(paths.Paths)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(root, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms)
VALUES('fixture','Fixture',0,1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms)
VALUES('fixture','Fixture',1,0,0);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('fixture','fixture',1)
`); err != nil {
		t.Fatal(err)
	}
	if err := installation.Reconcile(ctx, database.SQL, time.UnixMilli(1700000000000)); err != nil {
		t.Fatal(err)
	}
	var providerCount, targetCount int
	if err := database.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_providers`).Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_targets`).Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 1 || targetCount != 1 {
		t.Fatalf("projection counts = (%d,%d)", providerCount, targetCount)
	}
	response := httptest.NewRecorder()
	installation.Handler.ServeHTTP(response, httptest.NewRequestWithContext(ctx,
		http.MethodGet,
		"/runtime/providers/fixture/"+paths.bundleSHA256+"/client.mjs",
		nil,
	))
	if response.Code != http.StatusOK || response.Body.String() != "export{}" {
		t.Fatalf("provider response = %d %q", response.Code, response.Body.String())
	}
}

func TestLoadInstallationRejectsDescriptorManifestAndInstalledByteDrift(t *testing.T) {
	for _, mutate := range []func(t *testing.T, paths installationFixture){
		func(t *testing.T, paths installationFixture) {
			writeJSON(t, paths.activePath, map[string]any{"schemaVersion": 1})
		},
		func(t *testing.T, paths installationFixture) {
			writeFile(t, filepath.Join(paths.installationDir, "provider.json"), "{}")
		},
		func(t *testing.T, paths installationFixture) {
			writeFile(t, filepath.Join(paths.installationDir, "client.mjs"), "tampered")
		},
	} {
		t.Run("invalid", func(t *testing.T) {
			paths := writeInstallationFixture(t, t.TempDir())
			mutate(t, paths)
			if _, err := LoadInstallation(paths.Paths); err == nil {
				t.Fatal("invalid installation accepted")
			}
		})
	}
}

func TestLoadInstallationOverlaysPFBDevModuleWithoutChangingBaseBundle(t *testing.T) {
	root := t.TempDir()
	paths := writeInstallationFixture(t, root)
	devRoot := filepath.Join(root, "dev-provider")
	if err := os.MkdirAll(devRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	module := []byte("export const dev=true")
	devFiles := []devFileDescriptor{{
		Path: "client.mjs", SizeBytes: int64(len(module)), SHA256: sha256Hex(module),
		MediaType: "text/javascript; charset=utf-8",
	}}
	revision := developmentRevision("fixture", paths.bundleSHA256, devFiles)
	revisionRoot := filepath.Join(devRoot, "revisions", revision)
	if err := os.MkdirAll(revisionRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(revisionRoot, "client.mjs"), string(module))
	writeJSON(t, filepath.Join(devRoot, "dev-provider.json"), map[string]any{
		"schemaVersion":    1,
		"providerId":       "fixture",
		"baseBundleSha256": paths.bundleSHA256,
		"revision":         revision,
		"files": []map[string]any{{
			"path": "client.mjs", "sizeBytes": len(module), "sha256": sha256Hex(module),
			"mediaType": "text/javascript; charset=utf-8",
		}},
	})
	paths.DevRoot = devRoot
	installation, err := LoadInstallation(paths.Paths)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	installation.Handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/runtime/providers/fixture/"+paths.bundleSHA256+"/client.mjs", nil))
	if response.Code != http.StatusOK || response.Body.String() != string(module) ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dev module response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	envelope, err := installation.Builder.Build(runtimelaunch.Input{
		Binding: runtimecatalog.Binding{ProviderID: "fixture", TargetID: "fixture", LaunchPolicy: "SUPPORTED"},
		Session: runtimelaunch.Session{
			ID: "0198abcd-1234-7123-8abc-1234567890ab", Purpose: "PRODUCT", Mode: "SINGLE",
			Title: "Fixture", PlatformName: "Fixture", CoreName: "Fixture", ReturnTo: "/games/fixture",
		},
		Resources: []map[string]any{{
			"role": "game", "kind": "ROM_BLOB", "url": "/runtime/content/game",
			"ordinal": 0, "rangeRequired": false, "sha256": strings.Repeat("d", 64), "sizeBytes": 3,
		}},
		TargetOptions: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(envelope, &decoded); err != nil {
		t.Fatal(err)
	}
	runtimeValue, ok := decoded["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime = %#v", decoded["runtime"])
	}
	if runtimeValue["bundleSha256"] != paths.bundleSHA256 || runtimeValue["moduleSha256"] != sha256Hex(module) ||
		runtimeValue["moduleUrl"] != "/runtime/providers/fixture/"+paths.bundleSHA256+"/client.mjs" {
		t.Fatalf("dev launch runtime = %#v", runtimeValue)
	}
}

type installationFixture struct {
	Paths
	bundleSHA256    string
	activePath      string
	installationDir string
}

func writeInstallationFixture(t *testing.T, root string) installationFixture {
	t.Helper()
	module := []byte("export{}")
	moduleDigest := sha256Hex(module)
	providerJSON := []byte(`{"schemaVersion":1,"providerId":"fixture","providerVersion":"1.0.0","providerApiVersion":1,"clientModulePath":"client.mjs","targets":[{"id":"fixture","displayName":"Fixture","gameCompatibilityLine":"fixture-v1","netplayCompatibilityLine":null,"targetOptionsSchema":{"type":"object","additionalProperties":false,"properties":{},"required":[]},"inputs":[{"role":"game","kind":"ROM_BLOB","cardinality":"ONE","optional":false}],"capabilities":{"pause":false,"screenshot":false,"checkpoint":false,"standardGamepad":false,"frameCounter":false,"volume":false,"discSwitch":false,"nativeSettings":false,"inputFilter":false,"netplayPort":false,"videoModes":[],"requiresThreads":false,"frameMode":"NONE","validationProbes":[]},"checkpoint":null,"assetPaths":["client.mjs"]}]}`)
	providerDigest := sha256Hex(providerJSON)
	integrityValue := map[string]any{
		"schemaVersion": 1,
		"files": []map[string]any{
			{"path": "client.mjs", "sizeBytes": len(module), "sha256": moduleDigest, "mediaType": "text/javascript; charset=utf-8"},
			{"path": "provenance.json", "sizeBytes": 2, "sha256": sha256Hex([]byte("{}")), "mediaType": "application/json; charset=utf-8"},
			{"path": "provider.json", "sizeBytes": len(providerJSON), "sha256": providerDigest, "mediaType": "application/json; charset=utf-8"},
		},
	}
	integrityJSON, err := json.Marshal(integrityValue)
	if err != nil {
		t.Fatal(err)
	}
	bundle := strings.Repeat("a", 64)
	installationDir := filepath.Join(root, "installed", "fixture", bundle)
	if err := os.MkdirAll(installationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(installationDir, "client.mjs"), string(module))
	writeFile(t, filepath.Join(installationDir, "provider.json"), string(providerJSON))
	writeFile(t, filepath.Join(installationDir, "provenance.json"), "{}")
	writeFile(t, filepath.Join(installationDir, "integrity.json"), string(integrityJSON))

	manifest, err := runtimebundle.ParseManifest(providerJSON)
	if err != nil {
		t.Fatal(err)
	}
	integrity, err := runtimebundle.ParseIntegrity(integrityJSON)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = runtimebundle.BindTargetIntegrity(manifest, integrity.Files)
	if err != nil {
		t.Fatal(err)
	}
	target := manifest.Targets[0]
	activePath := filepath.Join(root, "active.json")
	writeJSON(t, activePath, map[string]any{
		"schemaVersion": 1, "source": "candidate", "sourceTreeSha256": strings.Repeat("e", 64), "release": nil,
		"providers": []map[string]any{{
			"providerId": "fixture", "providerVersion": "1.0.0", "providerApiVersion": 1,
			"bundleSha256": bundle, "bundleSizeBytes": 1, "manifestSha256": providerDigest,
			"moduleSha256": moduleDigest, "clientModulePath": "client.mjs",
			"installationPath": "fixture/" + bundle, "fileCount": 4,
			"unpackedSizeBytes": len(module) + len(providerJSON) + len(integrityJSON) + 2,
			"targets": []map[string]any{{
				"id": "fixture", "gameCompatibilityLine": "fixture-v1",
				"netplayCompatibilityLine": nil, "checkpoint": nil,
				"targetContractSha256": target.ContractSHA256,
			}},
		}},
	})
	catalogPath := filepath.Join(root, "catalog.json")
	writeJSON(t, catalogPath, map[string]any{
		"schemaVersion": 1, "catalogVersion": 1,
		"bindings": []map[string]any{{
			"id": "fixture", "coreId": "fixture", "providerId": "fixture", "targetId": "fixture",
			"platformIds": []string{"fixture"}, "acceptedContentKinds": []string{"SINGLE_FILE"},
			"detectorProfile": "FIXTURE", "deliveryProfile": "ROM_BLOB",
			"launchPolicy": "SUPPORTED", "reviewPolicy": "NONE",
		}},
	})
	return installationFixture{
		Paths:        Paths{ActivePath: activePath, InstalledRoot: filepath.Join(root, "installed"), CatalogPath: catalogPath},
		bundleSHA256: bundle, activePath: activePath, installationDir: installationDir,
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(contents))
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha256Hex(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
