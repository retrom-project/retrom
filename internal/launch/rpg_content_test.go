package launch

import (
	"errors"
	"testing"
)

func TestRPGContentPlanRequiresAdapterDerivedPayload(t *testing.T) {
	project := rpgLockedFile{fileRecord: "project", logicalName: "Data/Actors.rxdata", role: "PROJECT_FILE"}
	index := rpgLockedFile{fileRecord: "index", logicalName: "generated-index.json", role: "RPG_EASYRPG_INDEX"}
	plan, err := makeRPGContentPlan([]rpgLockedFile{project, index}, "RPG_EASYRPG_INDEX", false)
	if err != nil || len(plan.Files) != 2 || plan.Files[1].LogicalName != rpgEasyIndexName {
		t.Fatalf("EasyRPG content plan = %#v, error=%v", plan, err)
	}
	if _, err := makeRPGContentPlan([]rpgLockedFile{project}, "RPG_EASYRPG_INDEX", false); !errors.Is(err, ErrBlocked) {
		t.Fatalf("missing EasyRPG index error = %v", err)
	}
	reserved := rpgLockedFile{fileRecord: "reserved", logicalName: rpgEasyIndexName, role: "PROJECT_FILE"}
	if _, err := makeRPGContentPlan([]rpgLockedFile{reserved, index}, "RPG_EASYRPG_INDEX",
		false); !errors.Is(err, ErrBlocked) {
		t.Fatalf("reserved project path error = %v", err)
	}
}

func TestNativeRPGContentPlanPublishesOnlyWebRuntimeFiles(t *testing.T) {
	t.Parallel()
	files := []rpgLockedFile{
		{fileRecord: "entry", logicalName: "index.html", role: "PROJECT_FILE"},
		{fileRecord: "script", logicalName: "js/main.js", role: "PROJECT_FILE"},
		{fileRecord: "data", logicalName: "data/System.json", role: "PROJECT_FILE"},
		{fileRecord: "package", logicalName: "package.json", role: "PROJECT_FILE"},
		{fileRecord: "exe", logicalName: "Game.exe", role: "PROJECT_FILE"},
		{fileRecord: "dll", logicalName: "nw.dll", role: "PROJECT_FILE"},
		{fileRecord: "node", logicalName: "plugin.node", role: "PROJECT_FILE"},
		{fileRecord: "bat", logicalName: "launcher.bat", role: "PROJECT_FILE"},
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
