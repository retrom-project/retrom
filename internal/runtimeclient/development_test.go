package runtimeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestDevelopmentProviders(t *testing.T) {
	for _, scenario := range []string{"both", "duplicate", "wrong-base", "bad-hash"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			client := developmentTestClient()
			writeDevelopmentTestProvider(t, root, "dev/dev-provider.json", "retrom-runtime", "", "")
			provider, base, hash := "emulatorjs", "", ""
			switch scenario {
			case "duplicate":
				provider = "retrom-runtime"
			case "wrong-base":
				base = strings.Repeat("b", 64)
			case "bad-hash":
				hash = strings.Repeat("c", 64)
			}
			writeDevelopmentTestProvider(t, root, "dev/providers/emulatorjs/dev-provider.json", provider, base, hash)
			err := client.loadDevelopment(root)
			if scenario != "both" {
				if !errors.Is(err, model.ErrInvalid) {
					t.Fatalf("expected invalid development input, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"retrom-runtime", "emulatorjs"} {
				assertDevelopmentProvider(t, client, id)
			}
		})
	}
}

func developmentTestClient() *Client {
	client := &Client{
		Providers: map[string]Provider{}, Assets: map[string]map[string]bool{},
		Overrides: map[string]map[string]DevelopmentAsset{}, Fingerprints: map[string]string{},
	}
	for _, id := range []string{"retrom-runtime", "emulatorjs"} {
		client.Providers[id] = Provider{ProviderID: id, BundleSHA256: strings.Repeat("a", 64)}
		client.Assets[id] = map[string]bool{}
	}
	return client
}

func writeDevelopmentTestProvider(t *testing.T, root, path, id, base, hash string) {
	t.Helper()
	if base == "" {
		base = strings.Repeat("a", 64)
	}
	data := []byte("module:" + id)
	digest := sha256.Sum256(data)
	if hash == "" {
		hash = hex.EncodeToString(digest[:])
	}
	descriptor := map[string]any{
		"schemaVersion": 1, "providerId": id, "baseBundleSha256": base,
		"providerManifest":   map[string]any{"providerId": id},
		"targetFingerprints": map[string]string{"arcade": strings.Repeat("d", 64)},
		"files": []map[string]any{{
			"path": "client.mjs", "sha256": hash, "sizeBytes": len(data),
		}},
	}
	raw, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(root, path)
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	filesRoot := filepath.Join(filepath.Dir(path), "files")
	if err = os.MkdirAll(filesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filesRoot, hash), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertDevelopmentProvider(t *testing.T, client *Client, id string) {
	t.Helper()
	asset := client.Overrides[id]["client.mjs"]
	data, err := os.ReadFile(filepath.Join(asset.Root, asset.Name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "module:"+id {
		t.Fatalf("%s development module was not loaded", id)
	}
	if client.Fingerprints[id+"/arcade"] != strings.Repeat("d", 64) {
		t.Fatalf("%s development fingerprint was not loaded", id)
	}
}
