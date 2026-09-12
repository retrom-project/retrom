package serverimport_test

import (
	"reflect"
	"testing"
)

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
