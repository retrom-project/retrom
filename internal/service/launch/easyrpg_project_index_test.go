package launch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEasyRPGIndexPublishesCacheSizesWithoutLosingNativeLookup(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{"RPG2000", "RPG2003"} {
		for _, preview := range []bool{false, true} {
			database := "RPG_RT.ldb"
			if profile == "RPG2003" {
				database = "rpg_rt.ldb"
			}
			memory := indexMemoryFixture()
			memory.snapshot.Source.ContentKind = "RPG_MAKER_PROJECT"
			memory.snapshot.Source.DetectorProfile = profile
			memory.snapshot.Files = []ProjectIndexRecord{
				{Content: ConfigFile{LogicalName: database, Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("a", 64), Size: 16, Role: "GAME"}, Primary: true},
				{Content: ConfigFile{LogicalName: "CharSet/Hero #1.png", Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("b", 64), Size: 32, Role: "PROJECT_FILE"}},
				{Content: ConfigFile{LogicalName: "__retrom__/index.json", Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("c", 64), Size: 64, Role: "PROJECT_FILE"}},
			}
			if preview {
				memory.snapshot.Source.Purpose = "REVIEW_PREVIEW"
			}
			value, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), ProjectIndexReference{ID: "session"}, "valid")
			if err != nil {
				t.Fatalf("%s preview=%t: %v", profile, preview, err)
			}
			assertEasyRPGCacheIndex(t, value.Contents, database)
		}
	}
}

func assertEasyRPGCacheIndex(t *testing.T, contents []byte, database string) {
	t.Helper()
	var index struct {
		runtimeProjectIndex
		Cache    map[string]json.RawMessage `json:"cache"`
		Metadata struct {
			Version int `json:"version"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(contents, &index); err != nil {
		t.Fatal(err)
	}
	if index.SchemaVersion != 1 || index.Metadata.Version != 2 || len(index.Files) != 2 || string(index.Cache["rpg_rt.ldb"]) != `"`+database+`"` {
		t.Fatalf("invalid combined index: %s", contents)
	}
	for _, file := range index.Files {
		if file.Path == "CharSet/Hero #1.png" && (file.SizeBytes != 32 || !strings.HasSuffix(file.URL, "/CharSet/Hero%20%231.png")) {
			t.Fatalf("lost content identity: %+v", file)
		}
	}
}
