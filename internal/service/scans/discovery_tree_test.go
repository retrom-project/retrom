package scans

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentTreeReferencesStayWithTheSourceEntry(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"homebrew/EBOOT.PBP", "homebrew/script.bin", "homebrew/music/theme.ogg", "other/secret.bin"} {
		file := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	files, err := contentTreeReferences(root, map[string]string{"launch.pbp": "homebrew/EBOOT.PBP"}, []string{"launch.pbp"})
	if err != nil || len(files) != 3 {
		t.Fatalf("tree=%+v err=%v", files, err)
	}
	for _, file := range files {
		if !validContentReference(file, nil) || file.SourcePath == "other/secret.bin" {
			t.Fatalf("invalid tree reference: %+v", file)
		}
	}
	if _, err = contentTreeReferences(root, map[string]string{}, []string{"missing.pbp"}); err == nil {
		t.Fatal("unknown entry widened the source boundary")
	}
}
