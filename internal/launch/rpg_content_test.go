package launch

import (
	"errors"
	"testing"
)

func TestRPGContentPlanRequiresAdapterDerivedPayload(t *testing.T) {
	project := rpgLockedFile{blobID: "project", logicalName: "Data/Actors.rxdata", role: "PROJECT_FILE"}
	index := rpgLockedFile{blobID: "index", logicalName: "generated-index.json", role: "RPG_EASYRPG_INDEX"}
	plan, err := makeRPGContentPlan([]rpgLockedFile{project, index}, "RPG_EASYRPG_INDEX", false)
	if err != nil || len(plan.Files) != 2 || plan.Files[1].LogicalName != rpgEasyIndexName {
		t.Fatalf("EasyRPG content plan = %#v, error=%v", plan, err)
	}
	if _, err := makeRPGContentPlan([]rpgLockedFile{project}, "RPG_EASYRPG_INDEX", false); !errors.Is(err, ErrBlocked) {
		t.Fatalf("missing EasyRPG index error = %v", err)
	}
	reserved := rpgLockedFile{blobID: "reserved", logicalName: rpgEasyIndexName, role: "PROJECT_FILE"}
	if _, err := makeRPGContentPlan([]rpgLockedFile{reserved, index}, "RPG_EASYRPG_INDEX", false); !errors.Is(err, ErrBlocked) {
		t.Fatalf("reserved project path error = %v", err)
	}
}

func TestNativeRPGContentPlanPublishesOnlyWebRuntimeFiles(t *testing.T) {
	t.Parallel()
	files := []rpgLockedFile{
		{blobID: "entry", logicalName: "index.html", role: "PROJECT_FILE"},
		{blobID: "script", logicalName: "js/main.js", role: "PROJECT_FILE"},
		{blobID: "data", logicalName: "data/System.json", role: "PROJECT_FILE"},
		{blobID: "package", logicalName: "package.json", role: "PROJECT_FILE"},
		{blobID: "exe", logicalName: "Game.exe", role: "PROJECT_FILE"},
		{blobID: "dll", logicalName: "nw.dll", role: "PROJECT_FILE"},
		{blobID: "node", logicalName: "plugin.node", role: "PROJECT_FILE"},
		{blobID: "bat", logicalName: "launcher.bat", role: "PROJECT_FILE"},
	}
	plan, err := makeRPGContentPlan(files, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 3 {
		t.Fatalf("native runtime files = %#v", plan.Files)
	}
	for _, file := range plan.Files {
		if file.LogicalName != "index.html" && file.LogicalName != "js/main.js" &&
			file.LogicalName != "data/System.json" {
			t.Fatalf("desktop payload published: %#v", file)
		}
	}
	if _, err := makeRPGContentPlan(files[1:], "", true); !errors.Is(err, ErrBlocked) {
		t.Fatalf("native runtime without index error = %v", err)
	}
}
