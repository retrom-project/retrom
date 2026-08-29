package validation

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMachineRequiresCrossLaunchPositionRoundTrip(t *testing.T) {
	machine, restoreLaunchID, savePoint, restoreInput, sequence, now := prepareSuccessfulRestore(t)
	beginRestoreInput := Event{
		Sequence: sequence, EventID: uuid.NewString(), LaunchID: restoreLaunchID,
		Gate: GateRestoreInput, Phase: PhaseBegin, ObservedAtMS: now,
	}
	if err := machine.Apply(beginRestoreInput, ApplyContext{}); err != nil {
		t.Fatal(err)
	}
	sequence++
	now++
	unchanged := Event{
		Sequence: sequence, EventID: uuid.NewString(), LaunchID: restoreLaunchID,
		Gate: GateRestoreInput, Phase: PhasePass, ObservedAtMS: now, Position: &savePoint,
	}
	if err := machine.Apply(unchanged, ApplyContext{}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("unchanged restore input error=%v", err)
	}
	changed := unchanged
	changed.EventID = uuid.NewString()
	changed.Position = &restoreInput
	if err := machine.Apply(changed, ApplyContext{}); err != nil {
		t.Fatal(err)
	}
	if machine.State != StateAwaitingDecision || machine.Restored == nil || *machine.Restored != savePoint ||
		machine.RestoreInput == nil || *machine.RestoreInput != restoreInput {
		t.Fatalf("machine=%#v", machine)
	}
	note, err := machine.Decide(DecisionPass, "  verified\r\n位置  ")
	if err != nil || note != "verified\n位置" || machine.State != StatePassed {
		t.Fatalf("decision note=%q state=%s error=%v", note, machine.State, err)
	}
}

func prepareSuccessfulRestore(t *testing.T) (*Machine, string, Position, Position, int64, int64) {
	t.Helper()
	machine := newStartedMachine(t)
	initial := Position{MapID: 3, PlayerX: 9, PlayerY: 8, FixtureState: 0}
	savePoint := Position{MapID: 3, PlayerX: 10, PlayerY: 8, FixtureState: 1}
	diverged := Position{MapID: 3, PlayerX: 11, PlayerY: 8, FixtureState: 2}
	restoreInput := Position{MapID: 3, PlayerX: 12, PlayerY: 8, FixtureState: 3}
	sequence, now := int64(1), int64(1000)
	for _, gate := range gateOrder[:indexOfGate(GateOriginalLaunchEnded)+1] {
		positions := map[Gate]*Position{
			GateInitialPosition:       &initial,
			GateSavePointRecorded:     &savePoint,
			GatePostSaveStateDiverged: &diverged,
		}
		applyGate(t, machine, machine.LaunchID, gate, &sequence, &now, positions[gate], false)
	}
	if machine.State != StateCheckpointed {
		t.Fatalf("state=%s, want CHECKPOINTED", machine.State)
	}
	restoreLaunchID := uuid.NewString()
	if err := machine.AttachRestoreLaunch(restoreLaunchID); err != nil {
		t.Fatal(err)
	}
	applyGate(t, machine, restoreLaunchID, GateRestoreStarted, &sequence, &now, nil, false)
	applyGate(t, machine, restoreLaunchID, GateRestorePosition, &sequence, &now, &savePoint, false)
	applyGate(t, machine, restoreLaunchID, GateRestoreScreenshot, &sequence, &now, nil, true)
	if machine.State != StateRestored {
		t.Fatalf("state after screenshot=%s, want RESTORED", machine.State)
	}
	return machine, restoreLaunchID, savePoint, restoreInput, sequence, now
}

func TestMachineRejectsEqualInitialSaveAndUnchangedRestoreInput(t *testing.T) {
	machine := newStartedMachine(t)
	position := Position{MapID: 1, PlayerX: 5, PlayerY: 5, FixtureState: 1}
	sequence, now := int64(1), int64(1)
	for _, gate := range gateOrder[:indexOfGate(GateInitialPosition)+1] {
		var evidence *Position
		if gate == GateInitialPosition {
			evidence = &position
		}
		applyGate(t, machine, machine.LaunchID, gate, &sequence, &now, evidence, false)
	}
	begin := Event{
		Sequence: sequence, EventID: uuid.NewString(), LaunchID: machine.LaunchID,
		Gate: GateSavePointRecorded, Phase: PhaseBegin, ObservedAtMS: now,
	}
	if err := machine.Apply(begin, ApplyContext{}); err != nil {
		t.Fatal(err)
	}
	sequence++
	now++
	pass := Event{
		Sequence: sequence, EventID: uuid.NewString(), LaunchID: machine.LaunchID,
		Gate: GateSavePointRecorded, Phase: PhasePass, ObservedAtMS: now, Position: &position,
	}
	if err := machine.Apply(pass, ApplyContext{}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("equal A/B error=%v", err)
	}
}

func TestMachineRejectsSurfaceOnlyRestoreEvidence(t *testing.T) {
	tests := []struct {
		name         string
		diverged     Position
		restored     Position
		restoreSame  bool
		screenshotOK bool
	}{
		{name: "no divergence", diverged: Position{MapID: 1, PlayerX: 5, PlayerY: 5, FixtureState: 1}, restored: Position{MapID: 1, PlayerX: 5, PlayerY: 5, FixtureState: 1}, screenshotOK: true},
		{name: "wrong restored position", diverged: Position{MapID: 1, PlayerX: 6, PlayerY: 5, FixtureState: 2}, restored: Position{MapID: 1, PlayerX: 6, PlayerY: 5, FixtureState: 2}, screenshotOK: true},
		{name: "same launch", diverged: Position{MapID: 1, PlayerX: 6, PlayerY: 5, FixtureState: 2}, restored: Position{MapID: 1, PlayerX: 5, PlayerY: 5, FixtureState: 1}, restoreSame: true, screenshotOK: true},
		{name: "missing screenshot", diverged: Position{MapID: 1, PlayerX: 6, PlayerY: 5, FixtureState: 2}, restored: Position{MapID: 1, PlayerX: 5, PlayerY: 5, FixtureState: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if runInvalidRoundTrip(t, test) == nil {
				t.Fatal("surface-only restore evidence was accepted")
			}
		})
	}
}

func TestEventSequenceAndEventIDAreStrictAndIdempotent(t *testing.T) {
	machine := newStartedMachine(t)
	event := Event{Sequence: 1, EventID: uuid.NewString(), LaunchID: machine.LaunchID, Gate: GateRuntimeReady, Phase: PhaseBegin, ObservedAtMS: 1}
	if err := machine.Apply(event, ApplyContext{}); err != nil {
		t.Fatal(err)
	}
	if err := machine.Apply(event, ApplyContext{}); err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	changed := event
	changed.ObservedAtMS++
	if err := machine.Apply(changed, ApplyContext{}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("changed replay error=%v", err)
	}
	outOfOrder := Event{Sequence: 3, EventID: uuid.NewString(), LaunchID: machine.LaunchID, Gate: GateRuntimeReady, Phase: PhasePass, ObservedAtMS: 2}
	if err := machine.Apply(outOfOrder, ApplyContext{}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("out-of-order error=%v", err)
	}
}

func TestGateFailureIsTerminal(t *testing.T) {
	machine := newStartedMachine(t)
	sequence, now := int64(1), int64(1)
	begin := Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: machine.LaunchID, Gate: GateRuntimeReady, Phase: PhaseBegin, ObservedAtMS: now}
	if err := machine.Apply(begin, ApplyContext{}); err != nil {
		t.Fatal(err)
	}
	sequence++
	now++
	fail := Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: machine.LaunchID, Gate: GateRuntimeReady, Phase: PhaseFail, ObservedAtMS: now}
	if err := machine.Apply(fail, ApplyContext{}); err != nil {
		t.Fatal(err)
	}
	if machine.State != StateFailed || machine.FailureCode != "RPG_RUNTIME_GATE_RUNTIME_READY_FAILED" {
		t.Fatalf("machine=%#v", machine)
	}
}

func TestDecisionNoteNormalizationAndBounds(t *testing.T) {
	value, err := NormalizeDecisionNote("  Cafe\u0301\rtest  ")
	if err != nil || value != "Café\ntest" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	for _, invalid := range []string{"bad\x00note", string(make([]byte, 2001))} {
		if _, err := NormalizeDecisionNote(invalid); !errors.Is(err, ErrDecisionInvalid) {
			t.Fatalf("invalid note error=%v", err)
		}
	}
}

func newStartedMachine(t *testing.T) *Machine {
	t.Helper()
	machine, err := New(uuid.NewString(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err := machine.Start(); err != nil {
		t.Fatal(err)
	}
	return machine
}

func applyGate(
	t *testing.T,
	machine *Machine,
	launchID string,
	gate Gate,
	sequence, now *int64,
	position *Position,
	screenshotPersisted bool,
) {
	t.Helper()
	begin := Event{
		Sequence: *sequence, EventID: uuid.NewString(), LaunchID: launchID,
		Gate: gate, Phase: PhaseBegin, ObservedAtMS: *now,
	}
	if err := machine.Apply(begin, ApplyContext{}); err != nil {
		t.Fatalf("begin %s: %v", gate, err)
	}
	*sequence++
	*now++
	pass := Event{
		Sequence: *sequence, EventID: uuid.NewString(), LaunchID: launchID,
		Gate: gate, Phase: PhasePass, ObservedAtMS: *now, Position: position,
	}
	if err := machine.Apply(pass, ApplyContext{RestoreScreenshotPersisted: screenshotPersisted}); err != nil {
		t.Fatalf("pass %s: %v", gate, err)
	}
	*sequence++
	*now++
}

func runInvalidRoundTrip(t *testing.T, test struct {
	name         string
	diverged     Position
	restored     Position
	restoreSame  bool
	screenshotOK bool
},
) error {
	t.Helper()
	machine := newStartedMachine(t)
	initial := Position{MapID: 1, PlayerX: 4, PlayerY: 5, FixtureState: 0}
	save := Position{MapID: 1, PlayerX: 5, PlayerY: 5, FixtureState: 1}
	sequence, now := int64(1), int64(1)
	for _, gate := range gateOrder[:indexOfGate(GateCheckpointCreated)+1] {
		var position *Position
		if gate == GateInitialPosition {
			position = &initial
		}
		if gate == GateSavePointRecorded {
			position = &save
		}
		applyGate(t, machine, machine.LaunchID, gate, &sequence, &now, position, false)
	}
	if test.diverged == save {
		begin := Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: machine.LaunchID, Gate: GatePostSaveStateDiverged, Phase: PhaseBegin, ObservedAtMS: now}
		if err := machine.Apply(begin, ApplyContext{}); err != nil {
			return err
		}
		sequence++
		now++
		return machine.Apply(Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: machine.LaunchID, Gate: GatePostSaveStateDiverged, Phase: PhasePass, ObservedAtMS: now, Position: &test.diverged}, ApplyContext{})
	}
	applyGate(t, machine, machine.LaunchID, GatePostSaveStateDiverged, &sequence, &now, &test.diverged, false)
	applyGate(t, machine, machine.LaunchID, GateOriginalLaunchEnded, &sequence, &now, nil, false)
	restoreID := uuid.NewString()
	if test.restoreSame {
		restoreID = machine.LaunchID
	}
	if err := machine.AttachRestoreLaunch(restoreID); err != nil {
		return err
	}
	applyGate(t, machine, restoreID, GateRestoreStarted, &sequence, &now, nil, false)
	begin := Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: restoreID, Gate: GateRestorePosition, Phase: PhaseBegin, ObservedAtMS: now}
	if err := machine.Apply(begin, ApplyContext{}); err != nil {
		return err
	}
	sequence++
	now++
	if err := machine.Apply(Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: restoreID, Gate: GateRestorePosition, Phase: PhasePass, ObservedAtMS: now, Position: &test.restored}, ApplyContext{}); err != nil {
		return err
	}
	sequence++
	now++
	begin = Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: restoreID, Gate: GateRestoreScreenshot, Phase: PhaseBegin, ObservedAtMS: now}
	if err := machine.Apply(begin, ApplyContext{}); err != nil {
		return err
	}
	sequence++
	now++
	return machine.Apply(Event{Sequence: sequence, EventID: uuid.NewString(), LaunchID: restoreID, Gate: GateRestoreScreenshot, Phase: PhasePass, ObservedAtMS: now}, ApplyContext{RestoreScreenshotPersisted: test.screenshotOK})
}
