package serverimport_test

import (
	"reflect"
	"testing"
)

func TestServerImportItemCursorUsesTheSameByteOrderingAsItsPage(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	for _, name := range []string{"Z-fixture.zip", "a-fixture.zip"} {
		_, err := database.ExecContext(t.Context(), `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,logical_name,
requirement_mode,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms,
delivery_kind,emulator_path,archive_members_json)
SELECT ?,core_id,provider_id,target_id,source_kind,?,requirement_mode,catalog_digest,source_url,
source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,?,archive_members_json
FROM bios_requirements WHERE id='archive-fixture'`, name, name, "/same_cdi/bios/"+name)
		if err != nil {
			t.Fatal(err)
		}
	}
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"},
		"01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	var core, name, id string
	var seen []string
	for range 4 {
		page, err := service.Items(t.Context(), created.ID, "", "", "", core, name, id, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		seen = append(seen, page[0].LogicalName)
		core, name, id = page[0].CoreName, page[0].LogicalName, page[0].RequirementID
	}
	if !reflect.DeepEqual(seen, []string{"Z-fixture.zip", "a-fixture.zip", "fixture.zip"}) {
		t.Fatalf("item cursor duplicated or omitted mixed-case names: %v", seen)
	}
}

func TestServerImportCandidatePaginationKeepsNullRanksStable(t *testing.T) {
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, "01980000-0000-7000-8000-00000000b001")
	if err != nil {
		t.Fatal(err)
	}
	first, second := int64(1), int64(2)
	for _, candidate := range []struct {
		id   string
		rank *int64
	}{{"d", nil}, {"a", &second}, {"c", nil}, {"b", &first}} {
		if _, err := database.ExecContext(t.Context(), `INSERT INTO server_bios_import_candidates(id,server_import_id,requirement_id,relative_path,basename,association_kind,size_bytes,state,exact_basename,rank_ordinal,created_at_ms,updated_at_ms) VALUES(?,?,'archive-fixture',?,?,'EXACT_NAME',1,'DISCOVERED',1,?,1,1)`, candidate.id, created.ID, candidate.id, candidate.id, candidate.rank); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	var rank int64
	id := ""
	for range 3 {
		page, err := service.Candidates(t.Context(), created.ID, "archive-fixture", rank, id, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range page {
			seen = append(seen, candidate.ID)
		}
		if len(page) == 0 {
			break
		}
		last := page[len(page)-1]
		id = last.ID
		rank = 9223372036854775807
		if last.RankOrdinal != nil {
			rank = *last.RankOrdinal
		}
	}
	if !reflect.DeepEqual(seen, []string{"b", "a", "c", "d"}) {
		t.Fatalf("candidate ordering duplicated or omitted rows: %v", seen)
	}
}
