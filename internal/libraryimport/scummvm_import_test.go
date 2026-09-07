package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/blobstore"
	"retrom/internal/scummvm"
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
	raw := `{"schemaVersion":1,"upstreamCommit":"fed42f2068dcafc6aafa1c28c77e4c88def74b66","error":null,"candidates":[{"root":"Game","engineId":"sky","gameId":"sky","description":"Detected game","preferredTarget":"sky","language":"en","platform":"pc","extra":"Floppy","guiOptions":"","config":{},"canBeAdded":true,"isAddOn":false,"hasUnknownFiles":false,"supportLevel":0}]}`
	body := "#!/bin/sh\nfor arg do case \"$arg\" in --path=*) root=${arg#--path=};; esac; done\n[ -f \"$root/Game/opaque.bin\" ] || exit 2\nprintf '%s' '" + raw + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	detector := scummvm.New(func(context.Context) (scummvm.Tool, error) {
		return scummvm.Tool{Path: script, UpstreamCommit: "fed42f2068dcafc6aafa1c28c77e4c88def74b66", Engines: []string{"sky"}}, nil
	})
	service := New(nil, nil).WithBlobStore(blobs).WithScummVMDetector(detector)
	files := []importSourceFile{{id: "one", blobID: "one", path: "Game/opaque.bin", sha256: data.SHA256, size: data.Size}}
	_, groups, _, err := service.prepareScummVMProject(t.Context(), "DIRECTORY", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].validationStatus != "READY" || groups[0].sources[0].logicalName != "Game/opaque.bin" {
		t.Fatalf("groups=%+v", groups)
	}
	snapshot, err := scummvm.ParseSnapshot(groups[0].dependencySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := snapshot.Selected()
	if err != nil || candidate.Root != "Game" {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	var encoded map[string]any
	if json.Unmarshal([]byte(groups[0].dependencySnapshot), &encoded) != nil {
		t.Fatal("invalid snapshot")
	}
	if _, _, _, err := New(nil, nil).WithBlobStore(blobs).prepareScummVMProject(t.Context(), "DIRECTORY", files); err == nil {
		t.Fatal("missing detector became a successful empty scan")
	}
}
