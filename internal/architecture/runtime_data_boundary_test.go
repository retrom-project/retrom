package architecture

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRuntimeConsumersDoNotQueryImportOrReviewTables(t *testing.T) {
	t.Parallel()
	root := sourceRoot(t)
	workflowTable := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|INTO)\s+(?:import_|review_|source_import)[a-z_]+`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := "retrom/internal/" + filepath.ToSlash(filepath.Dir(relative))
		neutralPreviewRecord := strings.HasPrefix(filepath.ToSlash(relative), "persistence/recordstore/runtime_preview_")
		if !protectedRuntimePackage(name) && !neutralPreviewRecord {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match := workflowTable.Find(contents); match != nil {
			t.Errorf("runtime data dependency in %s: %s", relative, match)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
