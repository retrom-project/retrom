package metadatascrape

import (
	"testing"

	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

func TestPersistedEvidenceTerminalPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    metadatascrapemodel.WorkerEvidence
		terminal bool
	}{
		{"unattempted", metadatascrapemodel.WorkerEvidence{}, false},
		{"miss", metadatascrapemodel.WorkerEvidence{Attempts: 1, LastOutcome: metadatamodel.OutcomeMiss}, true},
		{"hit", metadatascrapemodel.WorkerEvidence{Attempts: 1, LastOutcome: metadatamodel.OutcomeHit}, true},
		{"invalid", metadatascrapemodel.WorkerEvidence{Attempts: 1, LastOutcome: metadatamodel.OutcomeInvalidResponse}, true},
		{"timeout remaining", metadatascrapemodel.WorkerEvidence{Attempts: 2, LastOutcome: metadatamodel.OutcomeTimeout}, false},
		{"network exhausted", metadatascrapemodel.WorkerEvidence{Attempts: 3, LastOutcome: metadatamodel.OutcomeNetworkError}, true},
		{"rate limit remaining", metadatascrapemodel.WorkerEvidence{Attempts: 1, LastOutcome: metadatamodel.OutcomeRateLimited}, false},
		{"cached terminal", metadatascrapemodel.WorkerEvidence{Attempts: 2, LastSource: "CACHE", LastOutcome: metadatamodel.OutcomeTimeout}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := evidenceTerminal(test.value); got != test.terminal {
				t.Fatalf("terminal=%v want=%v", got, test.terminal)
			}
		})
	}
}
