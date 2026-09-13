package emulationstationimport

import "testing"

func TestESCompanionsReachOwnedOrdinaryReviewAsExplicitDependencies(t *testing.T) {
	fixture, unit, item := companionFixture(t)
	if err := fixture.service.importExecutor().Process(fixture.context, unit, item); err != nil {
		t.Fatal(err)
	}
	var state, ordinaryID, jobID string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT execution_state,COALESCE(library_import_item_id,''),COALESCE(library_import_job_id,'') FROM emulationstation_import_items WHERE id=?`, item.ID).Scan(

		&state,

		&ordinaryID,

		&jobID,
	); err != nil {
		t.Fatal(
			err,
		)
	}
	if state != "REVIEW_PENDING" || ordinaryID == "" || jobID == "" {
		t.Fatalf("state=%s ordinary=%s job=%s", state, ordinaryID, jobID)
	}
	var companions, primary, games int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT (SELECT count(*) FROM import_item_source_files WHERE import_item_id=? AND role='COMPANION'),(SELECT count(*) FROM import_item_source_files WHERE import_item_id=? AND role='CONTENT'),(SELECT count(*) FROM games)`, ordinaryID, ordinaryID).Scan(

		&companions,

		&primary,

		&games,
	); err != nil {
		t.Fatal(
			err,
		)
	}
	if companions != 2 || primary != 1 || games != 0 {
		t.Fatalf("companions=%d primary=%d games=%d", companions, primary, games)
	}
}
