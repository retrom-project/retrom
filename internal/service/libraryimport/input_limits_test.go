package libraryimport

import (
	"testing"

	contentcapability "retrom/internal/content/capability"
	"retrom/internal/filestore"
)

func TestInputLimitUsesExpandedContentAndRetainsUnrelatedGroups(t *testing.T) {
	ordinal := 0
	policy := contentcapability.NewPolicy("SINGLE_FILE")
	policy.InputMaxFileBytes = map[string]int64{"game": 10}
	archive := ImportFile{ID: "archive", FileRecord: "zip", Path: "game.zip", Size: 3}
	valid := ImportFile{ID: "valid", FileRecord: "rom", Path: "other.bin", Size: 10, SHA256: "digest"}
	plan := PreparedImport{
		Target: ImportTarget{Policy: policy},
		Groups: []PreparedGroup{
			{Sources: []PreparedSource{{File: archive, Role: "CONTENT", LogicalName: "game.bin", ArchiveFileRecord: "zip", ArchiveOrdinal: &ordinal}}},
			{Sources: []PreparedSource{{File: valid, Role: "CONTENT", LogicalName: "other.bin"}}},
		},
		Archives:     []PreparedArchive{{FileRecord: "zip", Materialized: map[int]filestore.Metadata{0: {Size: 11, SHA256: "digest"}}}},
		Dispositions: []PreparedDisposition{{File: archive, Disposition: "SOURCE"}, {File: valid, Disposition: "SOURCE"}},
	}
	if err := applyPreparedInputLimits(&plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 1 || plan.Groups[0].Sources[0].File.ID != "valid" {
		t.Fatalf("groups=%+v", plan.Groups)
	}
	rejected := plan.Dispositions[0]
	if rejected.Rejection == nil || rejected.Rejection.RelativePath != "game.bin" || rejected.Rejection.Limit.Actual != 11 || rejected.Rejection.Limit.Maximum != 10 {
		t.Fatalf("rejected=%+v", rejected)
	}
	if plan.Dispositions[1].Disposition != "SOURCE" {
		t.Fatal("valid independent content rejected")
	}
}

func TestInputLimitNeverUsesContainerSizeForMissingExpandedMetadata(t *testing.T) {
	ordinal := 0
	policy := contentcapability.NewPolicy("SINGLE_FILE")
	policy.InputMaxFileBytes = map[string]int64{"game": 10}
	group := PreparedGroup{Sources: []PreparedSource{{File: ImportFile{Size: 1, SHA256: "archive"}, Role: "CONTENT", ArchiveOrdinal: &ordinal}}}
	if _, _, err := preparedGroupLimit(policy, group, nil); err == nil {
		t.Fatal("missing expanded identity accepted")
	}
}
