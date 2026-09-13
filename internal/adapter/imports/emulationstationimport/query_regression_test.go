package emulationstationimport

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestQueriesRejectInvalidStoredDiagnosticTypes(t *testing.T) {
	for _, tc := range []struct{ name, statement string }{
		{"flags", `UPDATE emulationstation_import_items SET source_flags_json='{"hidden":7}' WHERE import_id=?`},
		{"warnings", `UPDATE emulationstation_import_items SET warnings_json='[7]' WHERE import_id=?`},
		{"matches", `UPDATE emulationstation_import_items SET existing_matches_json='[{"gameId":7}]' WHERE import_id=?`},
		{"failure", `UPDATE emulationstation_import_items SET error_details_json='{"stage":7}' WHERE import_id=?`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			scanned := fixture.createAndScan(t)
			mustExecEmulationStationTest(t, fixture.database, tc.statement, scanned.ID)
			values, err := fixture.service.Items(fixture.context, scanned.ID, "", "", "", "", "", "", 10)
			var cause *json.UnmarshalTypeError
			if values != nil || !errors.As(err, &cause) {
				t.Fatalf("invalid diagnostic returned items=%#v error=%v", values, err)
			}
		})
	}
}

func TestGamelistsRejectInvalidStoredIgnoredFields(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	mustExecEmulationStationTest(t, fixture.database,
		`UPDATE emulationstation_import_gamelists SET ignored_fields_json='[7]' WHERE import_id=?`, scanned.ID)
	values, err := fixture.service.Gamelists(fixture.context, scanned.ID, "", "", 10)
	var cause *json.UnmarshalTypeError
	if values != nil || !errors.As(err, &cause) {
		t.Fatalf("invalid ignored fields returned gamelists=%#v error=%v", values, err)
	}
}
