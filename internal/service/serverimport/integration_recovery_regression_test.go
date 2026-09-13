package serverimport_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestCandidateRecoveryRejectsBrokenEvaluationEvidence(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	seedRecoveryCandidate(t, database, created.ID, "not-json")
	items, err := service.LoadItemsForTest(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LoadPersistedCandidatesForTest(t.Context(), created.ID, items); err == nil {
		t.Fatal("recovery silently discarded corrupt evaluation evidence")
	}
}

func TestCandidateRecoveryReleasesRowsBeforeLoadingDAT(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	seedRecoveryDAT(t, database)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	seedRecoveryCandidate(t, database, created.ID, "{}")
	items, err := service.LoadItemsForTest(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	groups, err := service.LoadPersistedCandidatesForTest(ctx, created.ID, items)
	if err != nil {
		t.Fatalf("single-connection recovery: %v", err)
	}
	values := groups["archive-fixture"]
	if len(values) != 1 || len(values[0].ExpectedDATEntries) != 1 || values[0].ExpectedDATEntries[0].Name != "boot.rom" || values[0].DAT == nil || !values[0].DAT.Launchable {
		t.Fatalf("incomplete recovered DAT candidate: %+v", values)
	}
}

func seedRecoveryCandidate(t *testing.T, database *sql.DB, importID, details string) {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `INSERT INTO server_bios_import_candidates(
 id,server_import_id,requirement_id,relative_path,basename,association_kind,size_bytes,state,
 exact_basename,safe_archive,launchable,matched_count,aliased_count,mismatched_count,missing_count,extra_count,
 evaluation_details_json,created_at_ms,updated_at_ms)
 VALUES('recovery-candidate',?,'archive-fixture','fixture.zip','fixture.zip','EXACT_NAME',1,'ELIGIBLE',
 1,1,1,1,0,0,0,0,?,1,1)`, importID, details)
	if err != nil {
		t.Fatal(err)
	}
}

func seedRecoveryDAT(t *testing.T, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `INSERT INTO dat_versions(id,core_id,provider_id,target_id,
 builtin_relative_path,sha256,parser_version,parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms)
 SELECT 'recovery-dat',core_id,provider_id,target_id,'fixture.xml',?,'1','READY',1,1,1,1,1
 FROM bios_requirements WHERE id='archive-fixture'`, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO dat_machines(dat_version_id,machine_name,description,
 year,manufacturer,is_explicit_bios,classification) VALUES('recovery-dat','fixture','Fixture','','',1,'EXPLICIT_BIOS')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,
 name,size_bytes,crc32,status) VALUES('recovery-dat','fixture',0,'boot.rom',1,'12345678','GOOD')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `UPDATE bios_requirements SET source_kind='DAT_MACHINE',
 archive_members_json=NULL,source_version='recovery-dat',dat_machine_name='fixture' WHERE id='archive-fixture'`)
	if err != nil {
		t.Fatal(err)
	}
}
