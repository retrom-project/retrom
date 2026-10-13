package runs

import (
	"context"
	"errors"
	"net/url"
	"path"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func TestResourceURLPreservesDriverNameAndEscapesProjectFilename(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"1941.zip", "项目/画面 #1.cue"} {
		file := blob(model.GameFile{LogicalKey: name})
		parsed, err := url.Parse(ResourceURL("run", file))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" || path.Base(parsed.Path) != path.Base(name) {
			t.Fatalf("resource URL changed the filename: %s", parsed)
		}
		if file.Filename != path.Base(name) || path.Base(parsed.Path) == file.ID {
			t.Fatal("opaque resource identity replaced the actual filename")
		}
	}
}

func TestPlanVirtualFilenamePreservesOriginalBlobIdentity(t *testing.T) {
	t.Parallel()
	original := model.GameFile{ID: "file-id", LogicalKey: "original.zip", SHA256: "raw-digest", SizeBytes: 42}
	byKey := map[string]model.GameFile{original.LogicalKey: original}
	for _, kind := range []string{"ROM_BLOB", "SEEKABLE_BLOB"} {
		plan := runtimeclient.Resource{
			Kind: kind, Files: []string{original.LogicalKey},
			Paths: map[string]string{original.LogicalKey: "1941.zip"},
		}
		selected, err := selectPlanFiles(byKey, plan)
		if err != nil {
			t.Fatal(err)
		}
		expected := blob(original)
		if len(selected) != 1 || selected[0].ID == "" {
			t.Fatal("run blob identity is missing")
		}
		expected.ID = selected[0].ID
		expected.Filename = "1941.zip"
		if len(selected) != 1 || selected[0] != expected || byKey[original.LogicalKey] != original {
			t.Fatal("virtual filename changed the original file, digest or logical key")
		}
		if path.Base(ResourceURL("run", selected[0])) != "1941.zip" {
			t.Fatal("runtime resource URL did not use the selected driver basename")
		}
	}
}

func TestPlanRejectsUnsafeBlobFilenameAndKeepsTreePaths(t *testing.T) {
	t.Parallel()
	byKey := map[string]model.GameFile{"original.zip": {LogicalKey: "original.zip"}}
	plan := runtimeclient.Resource{Kind: "ROM_BLOB", Files: []string{"original.zip"}}
	for _, name := range []string{"../game.zip", "/game.zip", "nested/game.zip"} {
		plan.Paths = map[string]string{"original.zip": name}
		if _, err := selectPlanFiles(byKey, plan); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("unsafe virtual basename %q accepted: %v", name, err)
		}
	}
	plan.Kind = "FILE_TREE"
	selected, err := selectPlanFiles(byKey, plan)
	if err != nil || len(selected) != 1 || selected[0].Filename != "original.zip" {
		t.Fatalf("file tree mapping was changed by blob naming: %v", err)
	}
}

func TestParentFileSetPreservesEachOriginalArchive(t *testing.T) {
	t.Parallel()
	files := []Blob{
		{ID: "base-id", Filename: "BASE.ZIP", LogicalPath: "stored/BASE.ZIP", SHA256: "base-digest", SizeBytes: 101},
		{ID: "grand-id", Filename: "grand.zip", LogicalPath: "stored/grand.zip", SHA256: "grand-digest", SizeBytes: 202},
	}
	plan := runtimeclient.Resource{
		Kind: "EXTERNAL_FILE_SET", Role: "parent",
		Paths: map[string]string{"stored/BASE.ZIP": "base.zip", "stored/grand.zip": "grand.zip"},
	}
	run := &Context{Run: model.Run{ID: "run-id"}}
	resource := map[string]any{}
	service := &Service{}
	// No runtime process or storage is available: delivery must use the original file locators.
	if err := service.resourcePlan(context.Background(), run, resource, files, plan, runtimeclient.Prepared{}); err != nil {
		t.Fatal(err)
	}
	actual, ok := resource["files"].([]map[string]any)
	if !ok || len(actual) != len(files) {
		t.Fatal("parent file set is missing")
	}
	for i, file := range files {
		if actual[i]["sha256"] != file.SHA256 || actual[i]["sizeBytes"] != file.SizeBytes ||
			actual[i]["url"] != ResourceURL(run.Run.ID, file) || actual[i]["logicalName"] != file.LogicalPath ||
			actual[i]["virtualPath"] != plan.Paths[file.LogicalPath] {
			t.Fatalf("parent archive identity changed: %#v", actual[i])
		}
	}
}
