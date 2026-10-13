package runtimeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentLargeCoreIsVerifiedFromDisk(t *testing.T) {
	root := t.TempDir()
	writeDevelopmentTestProvider(t, root, "dev/dev-provider.json", "retrom-runtime", "", "")
	path := filepath.Join(root, "dev", "dev-provider.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var descriptor map[string]any
	if err = json.Unmarshal(raw, &descriptor); err != nil {
		t.Fatal(err)
	}
	filesRoot := filepath.Join(root, "dev", "files")
	if err = os.MkdirAll(filesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(filesRoot, "large-core-")
	if err != nil {
		t.Fatal(err)
	}
	const size = 65 * 1024 * 1024
	if err = file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if err = os.Rename(file.Name(), filepath.Join(filesRoot, digest)); err != nil {
		t.Fatal(err)
	}
	files, ok := descriptor["files"].([]any)
	if !ok {
		t.Fatal("invalid fixture")
	}
	descriptor["files"] = append(files, map[string]any{
		"path": "assets/core.wasm", "sha256": digest, "sizeBytes": size,
	})
	raw, err = json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	client := developmentTestClient()
	if err = client.loadDevelopment(root); err != nil {
		t.Fatalf("large disk-backed core: %v", err)
	}
	if len(client.Overrides["retrom-runtime"]) != 2 {
		t.Fatal("missing development assets")
	}
}
