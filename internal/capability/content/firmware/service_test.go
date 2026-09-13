package firmware

import (
	"testing"

	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testassert"
)

func TestCompareArchiveEntriesClassifiesRequiredAndExtraFiles(t *testing.T) {
	t.Parallel()
	expected := []ExpectedDATEntry{
		{Name: "exact.bin", SizeBytes: 5, CRC32: "11111111"},
		{Name: "alias.bin", SizeBytes: 6, CRC32: "22222222"},
		{Name: "wrong.bin", SizeBytes: 7, CRC32: "33333333"},
		{Name: "missing.bin", SizeBytes: 8, CRC32: "44444444"},
	}
	actual := []importing.ArchiveEntry{
		{NormalizedPath: "exact.bin", Size: 5, CRC32: "11111111"},
		{NormalizedPath: "renamed.bin", Size: 6, CRC32: "22222222"},
		{NormalizedPath: "wrong.bin", Size: 70, CRC32: "33333333"},
		{NormalizedPath: "extra.bin", Size: 9, CRC32: "55555555"},
	}

	comparisons, missing, mismatched, warnings := CompareArchiveEntries(expected, actual)
	testassert.Falsef(t, len(comparisons) != 5, "comparisons = %#v", comparisons)
	wantedStatuses := []string{"MATCHED", "ALIASED", "MISMATCHED", "MISSING", "EXTRA"}
	for index, wanted := range wantedStatuses {
		testassert.Falsef(t, comparisons[index].Status != wanted, "comparison %d status = %q, want %q", index, comparisons[index].Status, wanted)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(missing) != 1 }, func() bool { return len(mismatched) != 1 }, func() bool { return len(warnings) != 1 }), "missing=%#v mismatched=%#v warnings=%#v", missing, mismatched, warnings)
	testassert.Falsef(t, testassert.Any(func() bool { return comparisons[1].Expected == nil }, func() bool { return comparisons[1].Actual == nil }, func() bool { return comparisons[1].Expected.Name != "alias.bin" }, func() bool { return comparisons[1].Actual.Name != "renamed.bin" }), "alias comparison = %#v", comparisons[1])
	testassert.Falsef(t, testassert.Any(func() bool { return comparisons[3].Actual != nil }, func() bool { return comparisons[4].Expected != nil }), "missing/extra comparisons = %#v / %#v", comparisons[3], comparisons[4])
}

func TestCompareArchiveEntriesUsesSHA1BeforeCRC(t *testing.T) {
	t.Parallel()
	expected := []ExpectedDATEntry{{
		Name:      "bios.bin",
		SizeBytes: 4,
		CRC32:     "11111111",
		SHA1:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}
	actual := []importing.ArchiveEntry{{
		NormalizedPath: "bios.bin",
		Size:           4,
		CRC32:          "11111111",
		SHA1:           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}}

	comparisons, _, mismatched, _ := CompareArchiveEntries(expected, actual)
	testassert.Falsef(t, testassert.Any(func() bool { return len(mismatched) != 1 }, func() bool { return len(comparisons) != 1 }, func() bool { return comparisons[0].Status != "MISMATCHED" }), "comparisons=%#v mismatched=%#v", comparisons, mismatched)
}
