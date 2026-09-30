//go:build integration

package launch

import (
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/libraryimport"
	reviewrepo "retrom/internal/persistence/libraryimport"
	reviewservice "retrom/internal/service/libraryimport"
)

func assertScreenshotFollowsCurrentDirectory(t *testing.T, database dbapi.DB, importer *libraryimport.Service, itemID string) int64 {
	t.Helper()
	ctx := t.Context()
	details := reviewservice.NewReviewDetails(reviewrepo.NewReviewDetail(database))
	before, err := details.Get(ctx, itemID)
	if err != nil || before.RuntimeScreenshot == nil || !before.CanApprove {
		t.Fatalf("original screenshot projection=%+v error=%v", before, err)
	}
	const other = "01980000-0000-7000-8000-000000009994"
	if _, err := database.ExecContext(ctx, `INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms)
SELECT ?,platform_id,default_core_id,'Other directory','other-directory',0,1,1,0,0
FROM platform_instances WHERE id=?`, other, before.PlatformInstance.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := importer.PatchDraft(ctx, itemID, before.Version, libraryimport.DraftPatch{TargetPlatformInstanceID: ptr(other), TagIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := details.Get(ctx, itemID)
	if err != nil || current.RuntimeScreenshot != nil || current.CanApprove {
		t.Fatalf("screenshot survived target directory change: %+v error=%v", current, err)
	}
	if _, err := importer.Approve(ctx, itemID, moved.Version); err == nil {
		t.Fatal("approval accepted a screenshot from another target directory")
	}
	restored, err := importer.PatchDraft(ctx, itemID, moved.Version, libraryimport.DraftPatch{TargetPlatformInstanceID: ptr(before.PlatformInstance.ID), TagIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	current, err = details.Get(ctx, itemID)
	if err != nil || current.RuntimeScreenshot == nil || !current.CanApprove {
		t.Fatalf("same source and target did not restore Item screenshot: %+v error=%v", current, err)
	}
	return restored.Version
}
