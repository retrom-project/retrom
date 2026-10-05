package diagnostic

import (
	"encoding/json"
	"testing"
)

func TestRejectionPreservesEngineEvidenceAndCanBeCleared(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"ARCADE","missingEntries":["parent.bin"],"datVersionId":"dat"}`,
		`{"kind":"SCUMMVM","detection":{"sourceDigest":"content"}}`,
		`{"policy":"PROJECT_RESOURCES_ONLY","externalRTP":["RTP"]}`,
	} {
		failure := &Rejection{
			Code: "CONTENT_FILE_BYTES_EXCEEDED", RelativePath: "game.bin",
			Limit: &Limit{Metric: "FILE_BYTES", Actual: 11, Maximum: 10},
		}
		blocked, err := WithRejection(raw, failure)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot map[string]json.RawMessage
		if err := json.Unmarshal([]byte(blocked), &snapshot); err != nil || snapshot["contentRejection"] == nil {
			t.Fatalf("lost diagnostic: %s %v", blocked, err)
		}
		cleared, err := WithRejection(blocked, nil)
		if err != nil {
			t.Fatal(err)
		}
		var before, after any
		if json.Unmarshal([]byte(raw), &before) != nil || json.Unmarshal([]byte(cleared), &after) != nil {
			t.Fatal("invalid test JSON")
		}
		original, _ := json.Marshal(before)
		actual, _ := json.Marshal(after)
		if string(original) != string(actual) {
			t.Fatalf("engine facts changed: %s -> %s", raw, cleared)
		}
	}
}
