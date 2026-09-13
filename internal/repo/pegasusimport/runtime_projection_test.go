package pegasusimport

import (
	"database/sql"
	"testing"

	"retrom/internal/testkit/testassert"
)

func TestProjectRuntimeCheckReturnsActionableArcadeDependencies(t *testing.T) {
	t.Parallel()
	snapshot := `{"schemaVersion":1,"kind":"ARCADE","machine":"1944j","missingEntries":["1944.zip"],"mismatchedEntries":[],"dependencies":[{"kind":"PARENT","machine":"1944","requiredBy":"1944j","expectedLogicalName":"1944.zip","state":"MISSING","requiredEntries":["nffe.03"]}]}`
	result, err := projectRuntimeCheck(
		sql.NullString{String: "BLOCKED", Valid: true},
		sql.NullString{String: "LAUNCH_PARENT_MISSING", Valid: true},
		sql.NullString{String: "fbneo", Valid: true},
		sql.NullString{String: "FinalBurn Neo", Valid: true},
		sql.NullString{String: snapshot, Valid: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return result == nil }, func() bool { return result.Machine == nil }, func() bool { return *result.Machine != "1944j" }, func() bool { return len(result.MissingEntries) != 1 }, func() bool { return result.MissingEntries[0] != "1944.zip" }, func() bool { return len(result.Dependencies) != 1 }, func() bool { return result.Dependencies[0].ExpectedLogicalName != "1944.zip" }, func() bool { return len(result.Dependencies[0].RequiredEntries) != 1 }), "runtime check = %#v", result)
}

func TestProjectRuntimeCheckReturnsMissingBIOSAndDiscs(t *testing.T) {
	t.Parallel()
	snapshot := `{"schemaVersion":1,"kind":"STATIC","bios":[{"logicalName":"saturn_bios.bin","requirementMode":"REQUIRED","conditionCode":null,"installationStatus":null}],"multiDisc":{"missingEntries":[{"ordinal":2,"sourceReference":"Disc 2.chd"}]}}`
	result, err := projectRuntimeCheck(
		sql.NullString{String: "BLOCKED", Valid: true},
		sql.NullString{String: "LAUNCH_BIOS_MISSING", Valid: true},
		sql.NullString{String: "yabause", Valid: true},
		sql.NullString{String: "Yabause", Valid: true},
		sql.NullString{String: snapshot, Valid: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return result == nil }, func() bool { return len(result.BIOS) != 1 }, func() bool { return result.BIOS[0].LogicalName != "saturn_bios.bin" }, func() bool { return len(result.MissingDiscs) != 1 }, func() bool { return result.MissingDiscs[0].SourceReference != "Disc 2.chd" }), "runtime check = %#v", result)
}
