//go:build integration

package libraryimport

import (
	"testing"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
)

func assertPreviewReadsCurrentBIOS(t *testing.T, database dbapi.DB, _ *Service, itemID, biosFileRecord string) int64 {
	t.Helper()
	var version int64
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT review_version FROM import_items WHERE id=?`, itemID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	runtime, err := repository.ReadReviewRuntime(t.Context(), database, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Status != "READY" {
		t.Fatalf("current BIOS status=%s code=%s", runtime.Status, runtime.Code)
	}
	files, err := repository.ReadReviewRuntimeFiles(t.Context(), database, itemID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		if file.Role == "BIOS_BUNDLE" && file.FileRecord == biosFileRecord {
			found = true
		}
	}
	if !found {
		t.Fatal("installed BIOS absent from current runtime files")
	}
	var after int64
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT review_version FROM import_items WHERE id=?`, itemID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != version {
		t.Fatal("reading BIOS changed review version")
	}
	return version
}
