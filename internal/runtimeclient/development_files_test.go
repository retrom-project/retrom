package runtimeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
)

func TestDevelopmentAssetRejectsMissingChangedAndLinkedFiles(t *testing.T) {
	for _, scenario := range []string{"missing", "symlink", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			writeDevelopmentTestProvider(t, root, "dev/dev-provider.json", "retrom-runtime", "", "")
			digest := sha256.Sum256([]byte("module:retrom-runtime"))
			file := filepath.Join(root, "dev/files", hex.EncodeToString(digest[:]))
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "symlink":
				target := filepath.Join(root, "outside")
				if err := os.WriteFile(target, []byte("module:retrom-runtime"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, file); err != nil {
					t.Fatal(err)
				}
			case "truncated":
				if err := os.WriteFile(file, []byte("short"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := developmentTestClient().loadDevelopment(root)
			want := model.ErrInvalid
			if scenario == "missing" {
				want = os.ErrNotExist
			}
			if !errors.Is(err, want) {
				t.Fatalf("%s: %v", scenario, err)
			}
		})
	}
}
