package firmware

import (
	"testing"

	"retrom/internal/importing"
	"retrom/internal/testassert"
)

func TestStaticRankingNeverLetsSizeBeatExactHash(t *testing.T) {
	t.Parallel()
	expectedSize := int64(4)
	expectation := StaticExpectation{LogicalName: "bios.bin", SizeBytes: &expectedSize, SHA256: "good"}
	values := []StaticEvaluation{
		EvaluateStatic(expectation, FileFacts{RelativePath: "large/bios.bin", Basename: "bios.bin", SizeBytes: 99, SHA256: "bad"}),
		EvaluateStatic(expectation, FileFacts{RelativePath: "renamed.bin", Basename: "renamed.bin", SizeBytes: 4, SHA256: "good"}),
	}
	SortStatic(values)
	testassert.Falsef(t, testassert.Any(func() bool { return values[0].Method != "EXACT_HASH" }, func() bool { return values[0].Facts.RelativePath != "renamed.bin" }), "winner = %#v", values[0])
}

func TestDATRankingPrefersLaunchableArchive(t *testing.T) {
	t.Parallel()
	expected := []ExpectedDATEntry{
		{Name: "a.rom", SizeBytes: 1, CRC32: "a"},
		{Name: "b.rom", SizeBytes: 1, CRC32: "b"},
	}
	partial := EvaluateDAT("machine.zip", expected,
		FileFacts{RelativePath: "partial.zip", Basename: "machine.zip", SizeBytes: 10, SHA256: "a"},
		[]importing.ArchiveEntry{{NormalizedPath: "a.rom", Size: 1, CRC32: "a"}},
	)
	warning := EvaluateDAT("machine.zip", expected,
		FileFacts{RelativePath: "warning.zip", Basename: "machine.zip", SizeBytes: 8, SHA256: "b"},
		[]importing.ArchiveEntry{
			{NormalizedPath: "a.rom", Size: 1, CRC32: "wrong"},
			{NormalizedPath: "b.rom", Size: 1, CRC32: "b"},
		},
	)
	values := []DATEvaluation{partial, warning}
	SortDAT(values)
	testassert.Falsef(t, testassert.Any(func() bool { return !values[0].Launchable }, func() bool { return values[0].Method != "DAT_ENTRY_WARNING" }), "winner = %#v", values[0])
}
