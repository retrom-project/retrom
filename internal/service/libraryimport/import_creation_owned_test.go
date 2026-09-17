package libraryimport

import (
	model "retrom/internal/model/libraryimport"
	"testing"
)

func TestOwnedSourcePlanRetainsCompanionInsideSelectedGroup(t *testing.T) {
	t.Parallel()
	group := model.PreparedGroup{
		Sources: []model.PreparedSource{
			{Role: "CONTENT", File: model.ImportFile{Path: "child.zip"}},
			{Role: "COMPANION", File: model.ImportFile{Path: "parent.zip"}},
		},
	}
	plan := model.PreparedImport{Target: model.ImportTarget{Version: 1}, Groups: []model.PreparedGroup{group}}
	source := &model.OwnedImportCreation{
		Intent: model.SourceCreationIntent{Kind: model.SourceOwnerPegasus, PrimaryPaths: []string{"child.zip"}},
		Before: model.SourceCreationSnapshot{TargetVersion: 1},
	}
	if err := validateOwnedImportPlan(plan, source); err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups[0].Sources) != 2 || plan.Groups[0].Sources[1].Role != "COMPANION" {
		t.Fatalf("owned selection dropped dependency: %#v", plan.Groups)
	}
}
