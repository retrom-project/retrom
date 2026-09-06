package runtimebundle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckpointSemanticsSurviveActiveProviderParsing(t *testing.T) {
	t.Parallel()
	for _, semantics := range []string{`"GAME_SAVE"`, `"INSTANT"`} {
		payload := strings.Replace(validCandidateActive, `"maxBytes":1024`,
			`"semantics":`+semantics+`,"maxBytes":1024`, 1)
		active, err := ParseActiveDescriptor([]byte(payload))
		if err != nil {
			t.Fatalf("semantics %s rejected: %v", semantics, err)
		}
		checkpoint := active.Providers[0].Targets[0].Checkpoint
		if checkpoint == nil || checkpoint.Semantics != strings.Trim(semantics, `"`) {
			t.Fatal("checkpoint semantics were lost")
		}
		encoded, err := json.Marshal(checkpoint)
		if err != nil || !strings.Contains(string(encoded), `"semantics":`+semantics) {
			t.Fatalf("checkpoint round trip: %s, %v", encoded, err)
		}
	}
}

func TestCheckpointSemanticsRejectMalformedValues(t *testing.T) {
	t.Parallel()
	for _, semantics := range []string{`""`, `"MEMORY_COPY"`, `null`, `true`, `1`} {
		payload := strings.Replace(validCandidateActive, `"maxBytes":1024`,
			`"semantics":`+semantics+`,"maxBytes":1024`, 1)
		if _, err := ParseActiveDescriptor([]byte(payload)); err == nil {
			t.Fatalf("invalid semantics %s accepted", semantics)
		}
	}
}
