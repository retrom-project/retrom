package scans

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
)

func TestBiosSourceFileLimit(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "flat", true: "nested"}[nested], func(t *testing.T) {
			directory := biosSourceFixture(t, nested)
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot(root)
			files, err := sourceFiles(root, ".", 0)
			if err != nil || len(files) != 100000 {
				t.Fatalf("100000 BIOS files: count=%d error=%v", len(files), err)
			}
			if err = os.WriteFile(filepath.Join(directory, "overflow.bin"), []byte("BIOS source"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = sourceFiles(root, ".", 0); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("100001 BIOS files: error=%v", err)
			}
		})
	}
}

func biosSourceFixture(t *testing.T, nested bool) string {
	t.Helper()
	directory := t.TempDir()
	if !nested {
		writeBiosSourceFiles(t, directory, 100000)
		return directory
	}
	for index := range 10 {
		child := filepath.Join(directory, fmt.Sprintf("part-%d", index))
		if err := os.Mkdir(child, 0o700); err != nil {
			t.Fatal(err)
		}
		writeBiosSourceFiles(t, child, 10000)
	}
	return directory
}

func writeBiosSourceFiles(t *testing.T, directory string, count int) {
	t.Helper()
	for index := range count {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("bios-%06d.bin", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
