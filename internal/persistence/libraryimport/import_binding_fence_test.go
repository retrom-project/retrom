package libraryimport

import (
	"errors"
	"testing"

	dbapi "retrom/internal/database"
	service "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestImportBindingFenceUsesCurrentKindsAndInputLimits(t *testing.T) {
	db := metadataDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, db, "gba/mgba")
	metadataExec(t, db, `INSERT INTO runtime_target_input_limits(provider_id,target_id,role,max_file_bytes) VALUES('emulatorjs','mgba','game',100),('emulatorjs','mgba','bios',200)`)
	target, err := service.ReadImportTarget(t.Context(), BindImportFacts(db), instance)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	records := creationRecords{transaction: tx}
	plan := service.PreparedImport{Target: target}
	if err := records.fenceBinding(t.Context(), plan); err != nil {
		t.Fatalf("unchanged public input facts rejected: %v", err)
	}
	if _, err := tx.ExecContext(t.Context(), `UPDATE runtime_target_input_limits SET max_file_bytes=99 WHERE provider_id='emulatorjs' AND target_id='mgba' AND role='game'`); err != nil {
		t.Fatal(err)
	}
	if err := records.fenceBinding(t.Context(), plan); !errors.Is(err, service.ErrVersionConflict) {
		t.Fatalf("changed limit accepted: %v", err)
	}
}
