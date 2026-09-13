package libraryimport

import "testing"

func TestOwnedSourcePlanRetainsCompanionInsideSelectedGroup(t *testing.T) {
	t.Parallel()
	group := PreparedGroup{
		Sources: []PreparedSource{
			{Role: "CONTENT", File: ImportFile{Path: "child.zip"}},
			{Role: "COMPANION", File: ImportFile{Path: "parent.zip"}},
		},
	}
	plan := PreparedImport{Target: ImportTarget{Version: 1}, Groups: []PreparedGroup{group}}
	source := &OwnedImportCreation{
		Intent: SourceCreationIntent{Kind: SourceOwnerPegasus, PrimaryPaths: []string{"child.zip"}},
		Before: SourceCreationSnapshot{TargetVersion: 1},
	}
	if err := validateOwnedImportPlan(plan, source); err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups[0].Sources) != 2 || plan.Groups[0].Sources[1].Role != "COMPANION" {
		t.Fatalf("owned selection dropped dependency: %#v", plan.Groups)
	}
}
