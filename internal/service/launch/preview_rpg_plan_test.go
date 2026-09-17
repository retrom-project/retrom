package launch

import (
	"errors"
	model "retrom/internal/model/launch"
	"testing"
)

func TestRPGContentPlanRequiresAdapterDerivedPayload(t *testing.T) {
	project := model.PreviewFile{BlobID: "project", LogicalName: "Data/Actors.rxdata", Role: "PROJECT_FILE"}
	index := model.PreviewFile{BlobID: "index", LogicalName: "generated-index.json", Role: "RPG_EASYRPG_INDEX"}
	plan, err := RPGContentFiles([]model.PreviewFile{project, index}, "RPG_EASYRPG_INDEX", false)
	if err != nil || len(plan) != 2 || plan[1].LogicalName != rpgEasyIndexName {
		t.Fatalf("EasyRPG content plan = %#v, error=%v", plan, err)
	}
	if _, err := RPGContentFiles([]model.PreviewFile{project}, "RPG_EASYRPG_INDEX", false); !errors.Is(err, model.ErrBlocked) {
		t.Fatalf("missing EasyRPG index error = %v", err)
	}
	reserved := model.PreviewFile{BlobID: "reserved", LogicalName: rpgEasyIndexName, Role: "PROJECT_FILE"}
	if _, err := RPGContentFiles([]model.PreviewFile{reserved, index}, "RPG_EASYRPG_INDEX", false); !errors.Is(err, model.ErrBlocked) {
		t.Fatalf("reserved project path error = %v", err)
	}
}

func TestNativeRPGContentPlanPublishesOnlyWebRuntimeFiles(t *testing.T) {
	t.Parallel()
	files := []model.PreviewFile{
		{BlobID: "entry", LogicalName: "index.html", Role: "PROJECT_FILE"},
		{BlobID: "script", LogicalName: "js/main.js", Role: "PROJECT_FILE"},
		{BlobID: "data", LogicalName: "data/System.json", Role: "PROJECT_FILE"},
		{BlobID: "package", LogicalName: "package.json", Role: "PROJECT_FILE"},
		{BlobID: "exe", LogicalName: "Game.exe", Role: "PROJECT_FILE"},
		{BlobID: "dll", LogicalName: "nw.dll", Role: "PROJECT_FILE"},
		{BlobID: "node", LogicalName: "plugin.node", Role: "PROJECT_FILE"},
		{BlobID: "bat", LogicalName: "launcher.bat", Role: "PROJECT_FILE"},
	}
	plan, err := RPGContentFiles(files, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("native runtime files = %#v", plan)
	}
	for _, file := range plan {
		if file.LogicalName != "index.html" && file.LogicalName != "js/main.js" &&
			file.LogicalName != "data/System.json" {
			t.Fatalf("desktop payload published: %#v", file)
		}
	}
	if _, err := RPGContentFiles(files[1:], "", true); !errors.Is(err, model.ErrBlocked) {
		t.Fatalf("native runtime without index error = %v", err)
	}
}
