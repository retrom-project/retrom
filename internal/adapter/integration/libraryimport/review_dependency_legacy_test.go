package libraryimport

import (
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
)

func TestReviewDependencyCompatibilityKeepsScalarValues(t *testing.T) {
	t.Parallel()
	name, sha := "disc.bin", "digest"
	size := int64(123)
	value := &libraryimportmodel.ReviewMultiDisc{
		Entries:          []libraryimportmodel.MultiDiscEntry{{LogicalName: &name, SizeBytes: &size, SHA256: &sha}},
		LatestAttachment: &libraryimportmodel.MultiDiscAttachment{Version: 7, CreatedAtMS: 11, Diagnostics: []byte(`{}`)},
	}
	result := legacyMultiDiscProjection(value)
	entries, ok := result["entries"].([]map[string]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("legacy entries=%#v", result["entries"])
	}
	if entries[0]["logicalName"] != name || entries[0]["sizeBytes"] != size || entries[0]["sha256"] != sha {
		t.Fatalf("legacy scalar types changed: %#v", entries[0])
	}
	attachment, ok := result["latestAttachment"].(map[string]any)
	if !ok || attachment["version"] != int64(7) || attachment["createdAtMs"] != int64(11) {
		t.Fatalf("legacy attachment scalar types changed: %#v", result["latestAttachment"])
	}
}
