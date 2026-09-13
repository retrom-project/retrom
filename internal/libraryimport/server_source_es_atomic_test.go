//go:build integration

package libraryimport

import "testing"

func TestOwnedESSourceCommitsPermanentBindingWithOrdinaryReview(t *testing.T) {
	fixture, request := ownedESSourceFixture(t)
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("created=%+v err=%v", result, err)
	}
	var jobID, itemID, kind string
	err = fixture.database.QueryRowContext(fixture.ctx, `SELECT COALESCE(library_import_job_id,''),COALESCE(library_import_item_id,''),
 (SELECT review_handoff_kind FROM import_items WHERE id=?) FROM emulationstation_import_items WHERE id=?`,
		result.Items[0].ItemID, request.Intent.ItemID).Scan(&jobID, &itemID, &kind)
	if err != nil {
		t.Fatal(err)
	}
	if jobID != result.Created.ImportJobID || itemID != result.Items[0].ItemID || kind != "EMULATIONSTATION" {
		t.Fatalf("ordinary review committed without ES binding: job=%q item=%q kind=%q", jobID, itemID, kind)
	}
}
