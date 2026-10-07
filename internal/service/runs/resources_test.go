package runs

import (
	"net/url"
	"path"
	"testing"

	"retrom/internal/model"
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
