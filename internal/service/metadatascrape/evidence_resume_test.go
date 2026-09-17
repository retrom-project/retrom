package metadatascrape

import (
	model "retrom/internal/model/metadatascrape"
	"testing"

	"retrom/internal/adapter/metadata/hasheous"
)

func TestPersistedEvidenceTerminalPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    model.WorkerEvidence
		terminal bool
	}{
		{"unattempted", model.WorkerEvidence{}, false},
		{"miss", model.WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeMiss}, true},
		{"hit", model.WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeHit}, true},
		{"invalid", model.WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeInvalidResponse}, true},
		{"timeout remaining", model.WorkerEvidence{Attempts: 2, LastOutcome: hasheous.OutcomeTimeout}, false},
		{"network exhausted", model.WorkerEvidence{Attempts: 3, LastOutcome: hasheous.OutcomeNetworkError}, true},
		{"rate limit remaining", model.WorkerEvidence{Attempts: 1, LastOutcome: hasheous.OutcomeRateLimited}, false},
		{"cached terminal", model.WorkerEvidence{Attempts: 2, LastSource: "CACHE", LastOutcome: hasheous.OutcomeTimeout}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := evidenceTerminal(test.value); got != test.terminal {
				t.Fatalf("terminal=%v want=%v", got, test.terminal)
			}
		})
	}
}
