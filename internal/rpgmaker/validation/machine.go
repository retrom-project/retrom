package validation

import (
	"fmt"

	"github.com/google/uuid"
)

var gateOrder = []Gate{
	GateRuntimeReady,
	GateEngineProfile,
	GateFrames300,
	GateInput,
	GateAudio,
	GateInitialPosition,
	GateSavePointRecorded,
	GateCheckpointCreated,
	GatePostSaveStateDiverged,
	GateOriginalLaunchEnded,
	GateRestoreStarted,
	GateRestorePosition,
	GateRestoreScreenshot,
	GateRestoreInput,
}

// GateOrder returns the protocol order without exposing mutable package state.
func GateOrder() []Gate {
	return append([]Gate(nil), gateOrder...)
}

func New(validationID, launchID string) (*Machine, error) {
	if !validUUID(validationID) || !validUUID(launchID) || validationID == launchID {
		return nil, ErrProtocol
	}
	return &Machine{
		ValidationID: validationID, LaunchID: launchID, State: StateCreated,
		Gates: make(map[Gate]GateResult), events: make(map[string]Event),
	}, nil
}

func (machine *Machine) Start() error {
	if machine.State != StateCreated {
		return ErrInvalidState
	}
	machine.State = StateStarting
	return nil
}

func (machine *Machine) AttachRestoreLaunch(launchID string) error {
	if machine.State != StateCheckpointed || !machine.gatePassed(GateOriginalLaunchEnded) ||
		!validUUID(launchID) || launchID == machine.LaunchID {
		return ErrInvalidState
	}
	if machine.RestoreLaunchID != "" {
		if machine.RestoreLaunchID == launchID {
			return nil
		}
		return ErrInvalidState
	}
	machine.RestoreLaunchID = launchID
	return nil
}

func (machine *Machine) Apply(event Event, context ApplyContext) error {
	if prior, exists := machine.events[event.EventID]; exists {
		if equalEvent(prior, event) {
			return nil
		}
		return ErrProtocol
	}
	if err := machine.validateEvent(event, context); err != nil {
		return err
	}
	result := machine.Gates[event.Gate]
	if event.Phase == PhaseBegin {
		result.BeginAtMS = event.ObservedAtMS
		result.Phase = PhaseBegin
	} else {
		result.TerminalAtMS = event.ObservedAtMS
		result.Phase = event.Phase
	}
	machine.Gates[event.Gate] = result
	machine.LastSequence = event.Sequence
	machine.events[event.EventID] = cloneEvent(event)
	if event.Phase == PhaseFail {
		machine.State = StateFailed
		machine.FailureCode = failureCode(event.Gate)
		return nil
	}
	if event.Phase == PhasePass {
		machine.applyPass(event)
	}
	return nil
}

func (machine *Machine) Expire() error {
	if terminal(machine.State) {
		return ErrInvalidState
	}
	machine.State = StateExpired
	return nil
}

func (machine *Machine) validateEvent(event Event, context ApplyContext) error {
	if !machine.validEventEnvelope(event) {
		return ErrProtocol
	}
	gateIndex := indexOfGate(event.Gate)
	if gateIndex < 0 || !machine.eventUsesExpectedLaunch(event) {
		return ErrProtocol
	}
	if !machine.validGateTransition(event, gateIndex) {
		return ErrProtocol
	}
	if err := machine.validatePosition(event); err != nil {
		return err
	}
	if event.Gate == GateRestoreScreenshot && event.Phase == PhasePass && !context.RestoreScreenshotPersisted {
		return ErrProtocol
	}
	return nil
}

func (machine *Machine) validEventEnvelope(event Event) bool {
	return !terminal(machine.State) && validUUID(event.EventID) &&
		event.Sequence == machine.LastSequence+1 && event.ObservedAtMS > 0 && validPhase(event.Phase)
}

func (machine *Machine) validGateTransition(event Event, gateIndex int) bool {
	prior, exists := machine.Gates[event.Gate]
	if event.Phase == PhaseBegin {
		return !exists && machine.previousGatePassed(gateIndex)
	}
	return exists && prior.Phase == PhaseBegin && event.ObservedAtMS >= prior.BeginAtMS
}

func (machine *Machine) eventUsesExpectedLaunch(event Event) bool {
	gateIndex := indexOfGate(event.Gate)
	if gateIndex <= indexOfGate(GateOriginalLaunchEnded) {
		return event.LaunchID == machine.LaunchID && machine.RestoreLaunchID == ""
	}
	return machine.RestoreLaunchID != "" && event.LaunchID == machine.RestoreLaunchID
}

func (machine *Machine) previousGatePassed(index int) bool {
	if index == 0 {
		return machine.State == StateStarting
	}
	return machine.gatePassed(gateOrder[index-1])
}

func (machine *Machine) validatePosition(event Event) error {
	positionGate := event.Gate == GateInitialPosition || event.Gate == GateSavePointRecorded ||
		event.Gate == GatePostSaveStateDiverged || event.Gate == GateRestorePosition ||
		event.Gate == GateRestoreInput
	if event.Phase == PhasePass && positionGate {
		return machine.validatePassedPosition(event)
	}
	if event.Position != nil {
		return ErrProtocol
	}
	return nil
}

func (machine *Machine) validatePassedPosition(event Event) error {
	if !validPositionCoordinates(event.Position) {
		return ErrProtocol
	}
	if event.Gate == GateInitialPosition {
		return nil
	}
	if event.Gate == GateSavePointRecorded {
		if machine.Initial == nil || *event.Position == *machine.Initial {
			return ErrProtocol
		}
		return nil
	}
	if event.Gate == GatePostSaveStateDiverged {
		if machine.SavePoint == nil || *event.Position == *machine.SavePoint {
			return ErrProtocol
		}
		return nil
	}
	if event.Gate == GateRestorePosition {
		if !machine.validRestoredPosition(event.Position) {
			return ErrProtocol
		}
		return nil
	}
	if event.Gate != GateRestoreInput || machine.Restored == nil || *event.Position == *machine.Restored {
		return ErrProtocol
	}
	return nil
}

func validPositionCoordinates(position *Position) bool {
	return position != nil && position.MapID > 0 && position.MapID <= 2147483647 &&
		position.PlayerX >= 0 && position.PlayerX <= 2147483647 &&
		position.PlayerY >= 0 && position.PlayerY <= 2147483647 &&
		position.FixtureState >= -2147483648 && position.FixtureState <= 2147483647
}

func (machine *Machine) validRestoredPosition(position *Position) bool {
	return machine.Initial != nil && machine.SavePoint != nil && machine.Diverged != nil &&
		*position == *machine.SavePoint && *position != *machine.Initial && *position != *machine.Diverged &&
		machine.RestoreLaunchID != machine.LaunchID
}

func (machine *Machine) applyPass(event Event) {
	switch event.Gate {
	case GateRuntimeReady:
		machine.State = StateRunning
	case GateInitialPosition:
		machine.Initial = clonePosition(event.Position)
	case GateSavePointRecorded:
		machine.SavePoint = clonePosition(event.Position)
	case GateCheckpointCreated:
		machine.State = StateCheckpointed
	case GatePostSaveStateDiverged:
		machine.Diverged = clonePosition(event.Position)
	case GateRestorePosition:
		machine.Restored = clonePosition(event.Position)
		machine.State = StateRestored
	case GateRestoreInput:
		machine.RestoreInput = clonePosition(event.Position)
		machine.State = StateAwaitingDecision
	case GateEngineProfile, GateFrames300, GateInput, GateAudio, GateOriginalLaunchEnded, GateRestoreStarted:
		return
	case GateRestoreScreenshot:
		return
	}
}

func (machine *Machine) gatePassed(gate Gate) bool {
	return machine.Gates[gate].Phase == PhasePass
}

func indexOfGate(gate Gate) int {
	for index, candidate := range gateOrder {
		if candidate == gate {
			return index
		}
	}
	return -1
}

func failureCode(gate Gate) string {
	return fmt.Sprintf("RPG_RUNTIME_GATE_%s_FAILED", gate)
}

// FailureCode is the stable failure attached to a terminal gate failure.
func FailureCode(gate Gate) string {
	return failureCode(gate)
}

func terminal(state State) bool {
	return state == StatePassed || state == StateFailed || state == StateExpired
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func validPhase(value Phase) bool {
	return value == PhaseBegin || value == PhasePass || value == PhaseFail
}

func equalEvent(left, right Event) bool {
	if left.Sequence != right.Sequence || left.EventID != right.EventID || left.LaunchID != right.LaunchID ||
		left.Gate != right.Gate || left.Phase != right.Phase || left.ObservedAtMS != right.ObservedAtMS {
		return false
	}
	if left.Position == nil || right.Position == nil {
		return left.Position == nil && right.Position == nil
	}
	return *left.Position == *right.Position
}

func cloneEvent(event Event) Event {
	event.Position = clonePosition(event.Position)
	return event
}

func clonePosition(position *Position) *Position {
	if position == nil {
		return nil
	}
	copyValue := *position
	return &copyValue
}
