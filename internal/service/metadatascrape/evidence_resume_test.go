package metadatascrape

import (
	"testing"

	"retrom/internal/hasheous"
)

func TestPersistedEvidenceTerminalPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    WorkerEvidence
		terminal bool
	}{
		{"unattempted", WorkerEvidence{}, false},
		{"miss", WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeMiss}, true},
		{"hit", WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeHit}, true},
		{"invalid", WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeInvalidResponse}, true},
		{"timeout remaining", WorkerEvidence{Attempts: 2, LastOutcome: hasheous.OutcomeTimeout}, false},
		{"network exhausted", WorkerEvidence{Attempts: 3, LastOutcome: hasheous.OutcomeNetworkError}, true},
		{"rate limit remaining", WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeRateLimited}, false},
		{"cached terminal", WorkerEvidence{Attempts: 2, LastSource: "CACHE", LastOutcome: hasheous.OutcomeTimeout}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := evidenceTerminal(test.value); got != test.terminal {
				t.Fatalf("terminal=%v want=%v", got, test.terminal)
			}
		})
	}
}
