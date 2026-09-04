package libraryimport

import (
	"database/sql"
	"errors"
	"testing"

	"retrom/internal/testassert"
)

func TestPreliminaryQuickApprovalReadyRequiresStrictCurrentReadyEvidence(t *testing.T) {
	t.Parallel()
	ready := reviewBulkCandidate{
		title: "Strict Ready", contentKind: "SINGLE_FILE", platformVersion: 3,
		providerID: sql.NullString{String: "emulatorjs", Valid: true},
		targetID:   sql.NullString{String: "fceumm", Valid: true},
		contentPolicyJSON: sql.NullString{
			String: `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`,
			Valid:  true,
		},
		validationID:              sql.NullString{String: "validation", Valid: true},
		validationStatus:          sql.NullString{String: "READY", Valid: true},
		validationGeneration:      sql.NullInt64{Int64: prepublishGeneration, Valid: true},
		validationPlatformVersion: sql.NullInt64{Int64: 3, Valid: true},
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
		{"target unavailable", func(value *reviewBulkCandidate) { value.targetID.Valid = false }},
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

func TestNormalizeReviewBulkScopeRejectsMultipleSourceBatches(t *testing.T) {
	t.Parallel()
	_, err := normalizeReviewBulkScope(ReviewBulkScope{
		ImportJobID:     "01980000-0000-7000-8000-000000000001",
		PegasusImportID: "01980000-0000-7000-8000-000000000002",
	})
	testassert.Truef(t, errors.Is(err, ErrReviewBulkInvalidScope), "error = %v", err)
}
