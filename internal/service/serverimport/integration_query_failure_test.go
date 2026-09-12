package serverimport_test

import (
	"testing"
)

func TestServerImportQueriesRejectBrokenEvidence(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root", SourceRelativePath: ""}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE server_bios_import_items SET selection_details_json='not-json' WHERE server_import_id=? AND requirement_id='archive-fixture'`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Items(t.Context(), created.ID, "", "", "", "", "", "", 20); err == nil {
		t.Error("invalid selection evidence silently returned as empty details")
	}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO server_bios_import_candidates(id,server_import_id,requirement_id,relative_path,basename,association_kind,size_bytes,state,exact_basename,evaluation_details_json,created_at_ms,updated_at_ms) VALUES('candidate',?,'archive-fixture','fixture.zip','fixture.zip','EXACT_NAME',1,'DISCOVERED',1,'not-json',1,1)`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Candidates(t.Context(), created.ID, "archive-fixture", 0, "", 20); err == nil {
		t.Error("invalid candidate evidence silently returned as empty details")
	}
}

func TestServerImportQueriesRejectUnboundedLimit(t *testing.T) {
	service, _, _ := archiveImportFixture(t)
	if _, err := service.List(t.Context(), "", 0, "", -1); err == nil {
		t.Fatal("negative limit produced an unbounded server import query")
	}
}
