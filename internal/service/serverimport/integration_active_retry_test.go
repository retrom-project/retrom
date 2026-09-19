package serverimport_test

import (
	"errors"
	"testing"

	serverimportmodel "retrom/internal/model/serverimport"
)

func TestServerImportRetryRejectsAnotherActiveImport(t *testing.T) {
	service, database, failed := failedControlImport(t)
	active, err := service.Create(t.Context(), serverimportmodel.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Retry(t.Context(), failed.ID, failed.Version, controlActorID); !errors.Is(err, serverimportmodel.ErrNotRetryable) {
		t.Fatalf("active import did not produce retry conflict: %v", err)
	}
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM server_imports WHERE id=?`, active.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" {
		t.Fatalf("retry changed active import to %s", state)
	}
	assertControlUnchanged(t, database, failed.ID, "FAILED", failed.Version)
}
