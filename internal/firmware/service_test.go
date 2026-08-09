package firmware

import (
	"database/sql"
	"testing"

	"retrom/internal/importing"
)

func TestCompareArchiveEntriesClassifiesRequiredAndExtraFiles(t *testing.T) {
	t.Parallel()
	expected := []expectedArchiveEntry{
		{name: "exact.bin", size: 5, crc32: sql.NullString{String: "11111111", Valid: true}},
		{name: "alias.bin", size: 6, crc32: sql.NullString{String: "22222222", Valid: true}},
		{name: "wrong.bin", size: 7, crc32: sql.NullString{String: "33333333", Valid: true}},
		{name: "missing.bin", size: 8, crc32: sql.NullString{String: "44444444", Valid: true}},
	}
	actual := []importing.ArchiveEntry{
		{NormalizedPath: "exact.bin", Size: 5, CRC32: "11111111"},
		{NormalizedPath: "renamed.bin", Size: 6, CRC32: "22222222"},
		{NormalizedPath: "wrong.bin", Size: 70, CRC32: "33333333"},
		{NormalizedPath: "extra.bin", Size: 9, CRC32: "55555555"},
	}

	comparisons, missing, mismatched, warnings := compareArchiveEntries(expected, actual)
	if len(comparisons) != 5 {
		t.Fatalf("comparisons = %#v", comparisons)
	}
	wantedStatuses := []string{"MATCHED", "ALIASED", "MISMATCHED", "MISSING", "EXTRA"}
	for index, wanted := range wantedStatuses {
		if comparisons[index].Status != wanted {
			t.Fatalf("comparison %d status = %q, want %q", index, comparisons[index].Status, wanted)
		}
	}
	if len(missing) != 1 || len(mismatched) != 1 || len(warnings) != 1 {
		t.Fatalf("missing=%#v mismatched=%#v warnings=%#v", missing, mismatched, warnings)
	}
	if comparisons[1].Expected == nil || comparisons[1].Actual == nil ||
		comparisons[1].Expected.Name != "alias.bin" || comparisons[1].Actual.Name != "renamed.bin" {
		t.Fatalf("alias comparison = %#v", comparisons[1])
	}
	if comparisons[3].Actual != nil || comparisons[4].Expected != nil {
		t.Fatalf("missing/extra comparisons = %#v / %#v", comparisons[3], comparisons[4])
	}
}

func TestCompareArchiveEntriesUsesSHA1BeforeCRC(t *testing.T) {
	t.Parallel()
	expected := []expectedArchiveEntry{{
		name:  "bios.bin",
		size:  4,
		crc32: sql.NullString{String: "11111111", Valid: true},
		sha1:  sql.NullString{String: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Valid: true},
	}}
	actual := []importing.ArchiveEntry{{
		NormalizedPath: "bios.bin",
		Size:           4,
		CRC32:          "11111111",
		SHA1:           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}}

	comparisons, _, mismatched, _ := compareArchiveEntries(expected, actual)
	if len(mismatched) != 1 || len(comparisons) != 1 || comparisons[0].Status != "MISMATCHED" {
		t.Fatalf("comparisons=%#v mismatched=%#v", comparisons, mismatched)
	}
}
