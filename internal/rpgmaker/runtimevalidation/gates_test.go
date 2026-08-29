package runtimevalidation

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"

	rpgvalidation "retrom/internal/rpgmaker/validation"
)

func TestLoadGateEventsReadsSQLiteTextEvidence(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(context.Background(), `
CREATE TABLE rpgmaker_runtime_validation_gate_events(
 validation_id TEXT,sequence INTEGER,event_id TEXT,launch_id TEXT,gate TEXT,phase TEXT,
 observed_at_ms INTEGER,evidence_json TEXT
);
INSERT INTO rpgmaker_runtime_validation_gate_events VALUES(
 'validation',1,'event','launch','RUNTIME_READY','BEGIN',1000,'{}'
);`); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })
	events, err := loadGateEvents(context.Background(), transaction, "validation")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || string(events[0].Evidence) != "{}" {
		t.Fatalf("events = %#v", events)
	}
}

func TestRehydrateMachineAttachesRestoreBeforeFirstRestoreEvent(t *testing.T) {
	t.Parallel()
	validationID := "0198abcd-1234-7123-8abc-1234567890ab"
	originalLaunchID := "0198abcd-1234-7123-8abc-1234567890ac"
	restoreLaunchID := "0198abcd-1234-7123-8abc-1234567890ad"
	a := rpgvalidation.Position{MapID: 1, PlayerX: 10, PlayerY: 8, FixtureState: 0}
	b := rpgvalidation.Position{MapID: 1, PlayerX: 11, PlayerY: 8, FixtureState: 1}
	c := rpgvalidation.Position{MapID: 1, PlayerX: 12, PlayerY: 8, FixtureState: 2}
	events := originalGateEvents(originalLaunchID, a, b, c)
	machine, err := rehydrateMachine(validationRuntime{
		id: validationID, launchID: originalLaunchID, state: "CHECKPOINTED",
		restoreLaunchID: sql.NullString{String: restoreLaunchID, Valid: true},
	}, events)
	if err != nil {
		t.Fatal(err)
	}
	event := rpgvalidation.Event{
		Sequence: 21, EventID: "0198abcd-1234-7123-8abc-1234567890ae",
		LaunchID: restoreLaunchID, Gate: rpgvalidation.GateRestoreStarted,
		Phase: rpgvalidation.PhaseBegin, ObservedAtMS: 3000,
	}
	if err := machine.Apply(event, rpgvalidation.ApplyContext{}); err != nil {
		t.Fatalf("first restore event rejected: %v", err)
	}
}

func originalGateEvents(
	launchID string,
	a, b, c rpgvalidation.Position,
) []storedEvent {
	positions := map[rpgvalidation.Gate]*rpgvalidation.Position{
		rpgvalidation.GateInitialPosition:       &a,
		rpgvalidation.GateSavePointRecorded:     &b,
		rpgvalidation.GatePostSaveStateDiverged: &c,
	}
	events := make([]storedEvent, 0, 20)
	sequence := int64(0)
	for _, gate := range rpgvalidation.GateOrder()[:10] {
		for _, phase := range []rpgvalidation.Phase{rpgvalidation.PhaseBegin, rpgvalidation.PhasePass} {
			sequence++
			position := (*rpgvalidation.Position)(nil)
			if phase == rpgvalidation.PhasePass {
				position = positions[gate]
			}
			events = append(events, storedEvent{
				Sequence: sequence, EventID: fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence),
				LaunchID: launchID, Gate: gate, Phase: phase, ObservedAtMS: 1000 + sequence,
				Position: position,
			})
		}
	}
	return events
}
