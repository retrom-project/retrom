package serverimport_test

import (
	"errors"
	"testing"
)

func TestServerImportControlPreservesDatabaseFailures(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	if _, err := database.ExecContext(t.Context(), `DROP TABLE server_imports`); err != nil {
		t.Fatal(err)
	}
	_, _, cancelErr := service.Cancel(t.Context(), "missing", 1, "stop", "01980000-0000-7000-8000-00000000b001")
	if cancelErr == nil || errors.Is(cancelErr, ErrNotCancellable) {
		t.Errorf("cancel hid storage failure: %v", cancelErr)
	}
	_, retryErr := service.Retry(t.Context(), "missing", 1, "01980000-0000-7000-8000-00000000b001")
	if retryErr == nil || errors.Is(retryErr, ErrNotRetryable) {
		t.Errorf("retry hid storage failure: %v", retryErr)
	}
}

func TestQueuedServerImportCancellationKeepsCompletedItems(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	actor := "01980000-0000-7000-8000-00000000b001"
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_bios_import_items SET state='NOT_FOUND',outcome_code='BIOS_CANDIDATE_NOT_FOUND',completed_at_ms=?,updated_at_ms=? WHERE server_import_id=?`, created.UpdatedAtMS, created.UpdatedAtMS, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_imports SET evaluated_item_count=1,not_found_count=1 WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}

	result, pending, err := service.Cancel(t.Context(), created.ID, created.Version, "stop", actor)
	if err != nil || pending || result.State != "CANCELLED" || result.Counts.NotFound != 1 || result.Counts.Cancelled != 0 {
		t.Fatalf("cancel overwrote completed work: %+v pending=%v error=%v", result, pending, err)
	}
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM server_bios_import_items WHERE server_import_id=?`, created.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "NOT_FOUND" {
		t.Fatalf("completed outcome changed to %s", state)
	}
}
