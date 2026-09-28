package launch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNativeWebIndexIncludesFrozenFilesAndStableURLs(t *testing.T) {
	for _, format := range []string{"RPG_MAKER_PROJECT", "TYRANOSCRIPT_PROJECT"} {
		t.Run(format, func(t *testing.T) {
			memory := indexMemoryFixture()
			memory.snapshot.Source.ContentKind = format
			memory.snapshot.Source.Delivery = "ISOLATED_WEB_PROJECT"
			memory.snapshot.Files = []ProjectIndexRecord{
				{Content: ConfigFile{LogicalName: "index.html", Format: format, Digest: strings.Repeat("a", 64), Size: 16}},
				{Content: ConfigFile{LogicalName: "audio/雪.ogg", Format: format, Digest: strings.Repeat("b", 64), Size: 32}},
				{Content: ConfigFile{LogicalName: "data/empty.json", Format: format, Digest: strings.Repeat("c", 64), Size: 0}},
			}
			service := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) })
			first, err := service.Index(t.Context(), ProjectIndexReference{ID: "first"}, "valid")
			if err != nil {
				t.Fatal(err)
			}
			second, err := service.Index(t.Context(), ProjectIndexReference{ID: "second"}, "valid")
			if err != nil || string(first.Contents) != string(second.Contents) {
				t.Fatalf("identity changed across Launch: %v", err)
			}
			var document runtimeProjectIndex
			if err := json.Unmarshal(first.Contents, &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Files) != 3 || document.Files[0].SizeBytes != 32 || !strings.Contains(document.Files[0].URL, "/audio/%E9%9B%AA.ogg") {
				t.Fatalf("unexpected native index: %s", first.Contents)
			}
		})
	}
}

func TestNativeMZEffectsRemainInCacheProjection(t *testing.T) {
	for _, name := range []string{"effects/Slash.efkefc", "effects/Model.efkmodel"} {
		entry, allowed := projectIndexEntry(ConfigFile{LogicalName: name, Size: 123}, "ISOLATED_WEB_PROJECT", "RPG_MAKER_PROJECT")
		if !allowed || entry.MediaType != "application/octet-stream" || entry.Path != name || entry.SizeBytes != 123 {
			t.Fatalf("effect projection: allowed=%t entry=%+v", allowed, entry)
		}
	}
}
