package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	scummvmnative "retrom/internal/adapter/engine/scummvm"
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/scummvm"
	librarymodel "retrom/internal/model/libraryimport"
)

func TestScummVMDirectoryUsesUpstreamDetectorAndImmutableCompleteTree(t *testing.T) {
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := blobs.Put(bytes.NewReader([]byte("opaque game data")))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "detector")
	inputRecord := filepath.Join(t.TempDir(), "detector-input")
	raw := `{"schemaVersion":1,"upstreamCommit":"fed42f2068dcafc6aafa1c28c77e4c88def74b66","error":null,"candidates":[{"root":"Game","engineId":"sky","gameId":"sky","description":"Detected game","preferredTarget":"sky","language":"en","platform":"pc","extra":"Floppy","guiOptions":"","config":{},"canBeAdded":true,"isAddOn":false,"hasUnknownFiles":false,"supportLevel":0}]}`
	body := "#!/bin/sh\nfor arg do case \"$arg\" in --path=*) root=${arg#--path=};; esac; done\n[ -f \"$root/Game/opaque.bin\" ] || exit 2\nprintf '%s' \"$root\" > '" + inputRecord + "'\nprintf '%s' '" + raw + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	detector := scummvmnative.New(func(context.Context) (scummvmnative.Tool, error) {
		return scummvmnative.Tool{Path: script, UpstreamCommit: "fed42f2068dcafc6aafa1c28c77e4c88def74b66", Engines: []string{"sky"}}, nil
	})
	service := New(nil, nil).WithBlobStore(blobs).WithScummVMDetector(detector)
	files := []importSourceFile{{ID: "one", BlobID: "one", Path: "Game/opaque.bin", SHA256: data.SHA256, Size: data.Size}}
	_, groups, _, err := service.importPreparation().PrepareScummVMProject(t.Context(), "DIRECTORY", files)
	if err != nil {
		t.Fatal(err)
	}
	assertScummVMInputRemoved(t, inputRecord)
	if len(groups) != 1 || groups[0].ValidationStatus != "READY" || groups[0].Sources[0].LogicalName != "Game/opaque.bin" {
		t.Fatalf("groups=%+v", groups)
	}
	snapshot, err := scummvm.ParseSnapshot(groups[0].DependencySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := snapshot.Selected()
	if err != nil || candidate.Root != "Game" {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	var encoded map[string]any
	if json.Unmarshal([]byte(groups[0].DependencySnapshot), &encoded) != nil {
		t.Fatal("invalid snapshot")
	}
	if err := os.WriteFile(script, []byte(body+"exit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.importPreparation().PrepareScummVMProject(t.Context(), "DIRECTORY", files); !errors.Is(err, librarymodel.ErrScummVMToolFailed) {
		t.Fatalf("native failure lost its Model error identity: %v", err)
	}
	assertScummVMInputRemoved(t, inputRecord)
	if _, _, _, err := New(nil, nil).WithBlobStore(blobs).importPreparation().PrepareScummVMProject(t.Context(), "DIRECTORY", files); err == nil {
		t.Fatal("missing detector became a successful empty scan")
	}
}

func assertScummVMInputRemoved(t *testing.T, recordPath string) {
	t.Helper()
	root, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(string(root)) {
		t.Fatalf("detector input was not an absolute private root: %q", root)
	}
	if _, err := os.Lstat(string(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private detector input survived the preparation return: %v", err)
	}
}
