package emulationstationimport

import (
	"encoding/json"
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
)

func TestInterruptedReviewKeepsExistingOmittedWarningCount(t *testing.T) {
	warnings := make([]map[string]any, 63, 64)
	for index := range warnings {
		warnings[index] = map[string]any{"code": "FIELD_IGNORED", "field": "unknown"}
	}
	warnings = append(warnings, map[string]any{"code": "WARNING_LIMIT_REACHED", "omittedCount": 27})
	encoded, err := json.Marshal(warnings)
	if err != nil {
		t.Fatal(err)
	}
	result, err := AppendReviewMetadataWarnings(string(encoded), []libraryimportmodel.ServerMetadataWarning{{Code: "FIELD_TRUNCATED", Field: "description"}})
	if err != nil {
		t.Fatal(err)
	}
	var actual []map[string]any
	if err := json.Unmarshal([]byte(result), &actual); err != nil {
		t.Fatal(err)
	}
	if len(actual) != 64 || actual[63]["omittedCount"] != float64(28) {
		t.Fatalf("warnings=%s", result)
	}
}
