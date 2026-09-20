package launch

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Use the real producer: UTF-8 paths expand again in URLs and JSON escaping.
func TestContentIOUnicodeIndexProducer(t *testing.T) {
	const maximum = 10_000
	root := "/runtime/content/project/" + strings.Repeat("a", 64) + "/"
	part := strings.Repeat("雪😀&\"", 25)
	files := make([]runtimeProjectIndexFile, maximum)
	files[0] = runtimeProjectIndexFile{Path: "startup.tjs", SizeBytes: 1}
	for i := 1; i < maximum; i++ {
		files[i] = runtimeProjectIndexFile{Path: fmt.Sprintf("%s/%s/%s/%05d.xp3", part, part, part, i), SizeBytes: 1}
	}
	view, err := buildProjectIndexDocument(root, "", projectIndexPolicy{
		minimum: 1, maximum: maximum, allowEmpty: true, marker: "startup.tjs",
	}, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Contents) <= 16*1024*1024 || len(view.Contents) > 65536+maximum*(18*1024+8192) {
		t.Fatalf("unexpected producer size: %d", len(view.Contents))
	}
	var decoded runtimeProjectIndex
	if err := json.Unmarshal(view.Contents, &decoded); err != nil || len(decoded.Files) != maximum {
		t.Fatalf("decode producer: %v", err)
	}
	for _, file := range decoded.Files {
		path, err := url.PathUnescape(strings.TrimPrefix(file.URL, root))
		if err != nil || path != file.Path {
			t.Fatalf("project URL changed logical path: %v", err)
		}
	}
	// Optional export for the cross-repository runtime consumer regression. Only
	// these generated, project-owned bytes are written; ordinary tests need no env.
	if output := os.Getenv("RETROM_CONTENT_IO_INDEX_OUTPUT"); output != "" {
		if err := os.WriteFile(output, view.Contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
