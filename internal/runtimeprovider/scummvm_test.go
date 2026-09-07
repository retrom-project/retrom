package runtimeprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/runtimebundle"
)

func TestScummVMToolUsesVerifiedInstallationAndPrivateExecutableCopy(t *testing.T) {
	root := t.TempDir()
	payload := []byte("test executable bytes; never executed")
	path := filepath.Join(root, "detector")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	expected := runtimebundle.IntegrityFile{SHA256: digest(payload), SizeBytes: int64(len(payload))}
	executable, err := prepareNativeTool(path, filepath.Join(root, "cache"), expected)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(executable)
	if err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("executable permissions: %v %v", info, err)
	}
	if executable == path {
		t.Fatal("mutated installed provider file mode")
	}
	if _, err := prepareNativeTool(path, filepath.Join(root, "cache"), expected); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeTool(path, filepath.Join(root, "cache"), expected); err == nil {
		t.Fatal("accepted tampered source")
	}
}

func TestScummVMToolManifestKeepsPinnedEngineClosure(t *testing.T) {
	value := map[string]any{
		"schemaVersion": 1, "adapterAbi": "scummvm-host-v1", "upstreamCommit": strings.Repeat("a", 40),
		"engines": map[string]string{"sky": "plugins/libsky.so"}, "files": []any{},
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := parseScummVMToolManifest(raw)
	if err != nil || manifest.Engines["sky"] != "plugins/libsky.so" {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	value["engines"] = map[string]string{"sky": "../arbitrary.so"}
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseScummVMToolManifest(raw); err == nil {
		t.Fatal("accepted arbitrary plugin path")
	}
}
