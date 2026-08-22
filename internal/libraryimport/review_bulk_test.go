package libraryimport

import (
	"database/sql"
	"testing"

	"retrom/internal/testassert"
)

func TestPreliminaryQuickApprovalReadyRequiresStrictCurrentReadyEvidence(t *testing.T) {
	t.Parallel()
	ready := reviewBulkCandidate{
		title: "Strict Ready", contentKind: "SINGLE_FILE", platformVersion: 3,
		artifactID: sql.NullString{String: "artifact", Valid: true},
		artifactCompatibility: sql.NullString{
			String: `{"schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"]}`,
			Valid:  true,
		},
		artifactVersion:           sql.NullInt64{Int64: 4, Valid: true},
		validationID:              sql.NullString{String: "validation", Valid: true},
		validationStatus:          sql.NullString{String: "READY", Valid: true},
		validationGeneration:      sql.NullInt64{Int64: prepublishGeneration, Valid: true},
		validationPlatformVersion: sql.NullInt64{Int64: 3, Valid: true},
		validationArtifactVersion: sql.NullInt64{Int64: 4, Valid: true},
	}
	testassert.True(t, preliminaryQuickApprovalReady(ready), "current READY evidence was rejected")

	tests := []struct {
		name   string
		mutate func(*reviewBulkCandidate)
	}{
		{"screenshot override remains manual", func(value *reviewBulkCandidate) {
			value.validationStatus.String = "BLOCKED"
			value.screenshotCurrent = true
		}},
		{"platform drift", func(value *reviewBulkCandidate) { value.validationPlatformVersion.Int64++ }},
		{"artifact drift", func(value *reviewBulkCandidate) { value.validationArtifactVersion.Int64++ }},
		{"generation drift", func(value *reviewBulkCandidate) { value.validationGeneration.Int64-- }},
		{"invalid title", func(value *reviewBulkCandidate) { value.title = "\n" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := ready
			test.mutate(&candidate)
			testassert.False(t, preliminaryQuickApprovalReady(candidate), "non-strict candidate was accepted")
		})
	}
}
