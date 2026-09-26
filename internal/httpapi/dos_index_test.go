package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/launch"
)

func TestDOSIndexDescribesVirtualZIP(t *testing.T) {
	content := launch.ContentView{
		Digest: strings.Repeat("a", 64), Format: "RETROM_DOS_DIRECT_ZIP_V1",
		CoreID: "dosbox_pure", ProviderID: "emulatorjs", TargetID: "dosbox-pure",
		BundleSHA256: strings.Repeat("b", 64),
	}
	data, err := dosGameIndex(content, 1537)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		SchemaVersion int `json:"schemaVersion"`
		Files         []struct {
			Path      string `json:"path"`
			URL       string `json:"url"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	identity, err := launch.ContentIdentity(content)
	if err != nil {
		t.Fatal(err)
	}
	url, err := launch.RuntimeContentURL("game", identity, "game.zip")
	if err != nil || index.SchemaVersion != 1 || len(index.Files) != 1 ||
		index.Files[0].Path != "game.zip" || index.Files[0].URL != url || index.Files[0].SizeBytes != 1537 {
		t.Fatalf("DOS virtual ZIP index = %+v, URL error = %v", index, err)
	}
}
