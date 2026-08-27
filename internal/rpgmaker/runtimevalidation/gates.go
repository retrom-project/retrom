package runtimevalidation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	rpgvalidation "retrom/internal/rpgmaker/validation"
	retromruntime "retrom/internal/runtime"
)

type validationRuntime struct {
	id, launchID, generation, adapterID, state string
	restoreLaunchID                            sql.NullString
	lastSequence, expiresAtMS                  int64
	evidenceScreenshot                         sql.NullString
}

type storedEvent struct {
	Sequence     int64
	EventID      string
	LaunchID     string
	Gate         rpgvalidation.Gate
	Phase        rpgvalidation.Phase
	ObservedAtMS int64
	Evidence     json.RawMessage
	Position     *rpgvalidation.Position
}

func (service *Service) ApplyGate(
	ctx context.Context,
	launchID, capability string,
	request GateRequest,
) (GateAccepted, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return GateAccepted{}, fmt.Errorf("begin RPG gate event: %w", err)
	}
	defer cleanup.Rollback(transaction)
	runtime, err := service.authorizeGateLaunch(ctx, transaction, launchID, capability)
	if err != nil {
		return GateAccepted{}, err
	}
	canonicalEvidence, position, err := service.validateGateEvidence(ctx, transaction, runtime, request)
	if err != nil {
		return GateAccepted{}, err
	}
	request.Evidence = canonicalEvidence
	accepted, found, err := replayAndCommitGate(ctx, transaction, runtime, launchID, request)
	if err != nil {
		return GateAccepted{}, err
	}
	if found {
		return accepted, nil
	}
	events, err := loadGateEvents(ctx, transaction, runtime.id)
	if err != nil {
		return GateAccepted{}, err
	}
	machine, err := rehydrateMachine(runtime, events)
	if err != nil {
		return GateAccepted{}, ErrProtocol
	}
	event := rpgvalidation.Event{
		Sequence: request.Sequence, EventID: request.EventID, LaunchID: launchID,
		Gate: rpgvalidation.Gate(request.Gate), Phase: rpgvalidation.Phase(request.Phase),
		ObservedAtMS: request.ObservedAtMS, Position: position,
	}
	if err := machine.Apply(event, rpgvalidation.ApplyContext{
		RestoreScreenshotPersisted: runtime.evidenceScreenshot.Valid,
	}); err != nil {
		return GateAccepted{}, ErrProtocol
	}
	return service.persistGateEvent(
		ctx, transaction, runtime, launchID, request, canonicalEvidence, position, events, machine,
	)
}

func (service *Service) persistGateEvent(
	ctx context.Context,
	transaction *sql.Tx,
	runtime validationRuntime,
	launchID string,
	request GateRequest,
	canonicalEvidence json.RawMessage,
	position *rpgvalidation.Position,
	events []storedEvent,
	machine *rpgvalidation.Machine,
) (GateAccepted, error) {
	createdAt := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, runtime.id, request.Sequence, request.EventID, launchID, request.Gate, request.Phase,
		request.ObservedAtMS, string(canonicalEvidence), createdAt); err != nil {
		return GateAccepted{}, ErrProtocol
	}
	events = append(events, storedEvent{
		Sequence: request.Sequence, EventID: request.EventID, LaunchID: launchID,
		Gate: rpgvalidation.Gate(request.Gate), Phase: rpgvalidation.Phase(request.Phase),
		ObservedAtMS: request.ObservedAtMS, Evidence: canonicalEvidence, Position: position,
	})
	machineGates, err := projectMachineGates(events)
	if err != nil {
		return GateAccepted{}, err
	}
	failureCode := any(nil)
	if machine.FailureCode != "" {
		failureCode = machine.FailureCode
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET state=?,last_gate_sequence=?,machine_gates_json=?,failure_code=?,updated_at_ms=?
WHERE id=? AND state=? AND last_gate_sequence=?
`, string(machine.State), machine.LastSequence, machineGates, failureCode, createdAt,
		runtime.id, runtime.state, runtime.lastSequence)
	if err != nil {
		return GateAccepted{}, ErrProtocol
	}
	if count, rowsErr := result.RowsAffected(); rowsErr != nil || count != 1 {
		return GateAccepted{}, ErrProtocol
	}
	if err := transaction.Commit(); err != nil {
		return GateAccepted{}, fmt.Errorf("commit RPG gate event: %w", err)
	}
	return GateAccepted{
		Sequence: request.Sequence, EventID: request.EventID,
		ValidationState: string(machine.State), IdempotentReplay: false,
	}, nil
}

func replayAndCommitGate(
	ctx context.Context,
	transaction *sql.Tx,
	runtime validationRuntime,
	launchID string,
	request GateRequest,
) (GateAccepted, bool, error) {
	accepted, found, err := replayGateEvent(ctx, transaction, runtime, launchID, request)
	if err != nil || !found {
		return accepted, found, err
	}
	if err := transaction.Commit(); err != nil {
		return GateAccepted{}, false, fmt.Errorf("commit replayed RPG gate event: %w", err)
	}
	return accepted, true, nil
}

func (service *Service) authorizeGateLaunch(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, capability string,
) (validationRuntime, error) {
	if !validCanonicalUUID(launchID) {
		return validationRuntime{}, ErrCredential
	}
	var runtime validationRuntime
	var credentialHash []byte
	var launchState, purpose string
	var hardExpires int64
	err := transaction.QueryRowContext(ctx, `
SELECT validation.id,validation.launch_id,validation.restore_launch_id,
 validation.generation,validation.adapter_id,validation.state,validation.last_gate_sequence,
 validation.expires_at_ms,validation.evidence_screenshot_blob_id,
 launch.credential_sha256,launch.state,launch.purpose,launch.hard_expires_at_ms
FROM launch_sessions launch
JOIN rpgmaker_runtime_validations validation ON validation.id=launch.rpgmaker_runtime_validation_id
WHERE launch.id=?
`, launchID).Scan(
		&runtime.id, &runtime.launchID, &runtime.restoreLaunchID, &runtime.generation, &runtime.adapterID,
		&runtime.state, &runtime.lastSequence, &runtime.expiresAtMS, &runtime.evidenceScreenshot,
		&credentialHash, &launchState, &purpose, &hardExpires,
	)
	now := service.now().UnixMilli()
	if err != nil || purpose != "RPG_RUNTIME_VALIDATION" ||
		!retromruntime.MatchesCapability(capability, credentialHash) || hardExpires <= now ||
		runtime.expiresAtMS <= now || launchState == "FINISHED" || launchState == "EXPIRED" ||
		launchState == "REVOKED" {
		return validationRuntime{}, ErrCredential
	}
	return runtime, nil
}

func replayGateEvent(
	ctx context.Context,
	transaction *sql.Tx,
	runtime validationRuntime,
	launchID string,
	request GateRequest,
) (GateAccepted, bool, error) {
	var existing storedEvent
	var gate, phase, evidence string
	err := transaction.QueryRowContext(ctx, `
SELECT sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json
FROM rpgmaker_runtime_validation_gate_events WHERE event_id=?
`, request.EventID).Scan(
		&existing.Sequence, &existing.EventID, &existing.LaunchID, &gate, &phase,
		&existing.ObservedAtMS, &evidence,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return GateAccepted{}, false, nil
	}
	if err != nil {
		return GateAccepted{}, false, fmt.Errorf("query RPG gate replay: %w", err)
	}
	if existing.Sequence != request.Sequence || existing.LaunchID != launchID || gate != request.Gate ||
		phase != request.Phase || existing.ObservedAtMS != request.ObservedAtMS || evidence != string(request.Evidence) {
		return GateAccepted{}, false, ErrProtocol
	}
	return GateAccepted{
		Sequence: request.Sequence, EventID: request.EventID,
		ValidationState: runtime.state, IdempotentReplay: true,
	}, true, nil
}

func loadGateEvents(ctx context.Context, transaction *sql.Tx, validationID string) ([]storedEvent, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json
FROM rpgmaker_runtime_validation_gate_events
WHERE validation_id=? ORDER BY sequence
`, validationID)
	if err != nil {
		return nil, fmt.Errorf("query RPG gate events: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	events := make([]storedEvent, 0)
	for rows.Next() {
		var event storedEvent
		var gate, phase, evidence string
		if err := rows.Scan(
			&event.Sequence, &event.EventID, &event.LaunchID, &gate, &phase,
			&event.ObservedAtMS, &evidence,
		); err != nil {
			return nil, fmt.Errorf("scan RPG gate event: %w", err)
		}
		event.Gate, event.Phase = rpgvalidation.Gate(gate), rpgvalidation.Phase(phase)
		event.Evidence = json.RawMessage(evidence)
		if isPositionGate(event.Gate) && event.Phase == rpgvalidation.PhasePass {
			var position rpgvalidation.Position
			if err := strictEvidence(event.Evidence, &position); err != nil {
				return nil, ErrProtocol
			}
			event.Position = &position
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate RPG gate events: %w", err)
	}
	return events, nil
}

func rehydrateMachine(runtime validationRuntime, events []storedEvent) (*rpgvalidation.Machine, error) {
	machine, err := rpgvalidation.New(runtime.id, runtime.launchID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate RPG validation: %w", err)
	}
	if err := machine.Start(); err != nil {
		return nil, fmt.Errorf("start rehydrated RPG validation: %w", err)
	}
	attached := false
	for _, stored := range events {
		if indexOfGate(stored.Gate) >= indexOfGate(rpgvalidation.GateRestoreStarted) && !attached {
			if !runtime.restoreLaunchID.Valid || machine.AttachRestoreLaunch(runtime.restoreLaunchID.String) != nil {
				return nil, ErrProtocol
			}
			attached = true
		}
		if err := machine.Apply(rpgvalidation.Event{
			Sequence: stored.Sequence, EventID: stored.EventID, LaunchID: stored.LaunchID,
			Gate: stored.Gate, Phase: stored.Phase, ObservedAtMS: stored.ObservedAtMS,
			Position: stored.Position,
		}, rpgvalidation.ApplyContext{RestoreScreenshotPersisted: true}); err != nil {
			return nil, fmt.Errorf("apply rehydrated RPG validation event: %w", err)
		}
	}
	if runtime.restoreLaunchID.Valid && !attached {
		if err := machine.AttachRestoreLaunch(runtime.restoreLaunchID.String); err != nil {
			return nil, ErrProtocol
		}
	}
	return machine, nil
}

func projectMachineGates(events []storedEvent) (string, error) {
	byGate := make(map[rpgvalidation.Gate][]storedEvent)
	for _, event := range events {
		byGate[event.Gate] = append(byGate[event.Gate], event)
	}
	result := make([]MachineGate, 0, len(rpgvalidation.GateOrder()))
	for _, gate := range rpgvalidation.GateOrder() {
		projection := MachineGate{Gate: string(gate), Status: GateNotStarted}
		for _, event := range byGate[gate] {
			observed := event.ObservedAtMS
			switch event.Phase {
			case rpgvalidation.PhaseBegin:
				projection.Status, projection.BegunAtMS = GateInProgress, &observed
			case rpgvalidation.PhasePass:
				projection.Status, projection.CompletedAtMS = GatePassed, &observed
				projection.Evidence = cloneRaw(event.Evidence)
			case rpgvalidation.PhaseFail:
				projection.Status, projection.CompletedAtMS = GateFailed, &observed
				projection.Evidence = cloneRaw(event.Evidence)
				failure := rpgvalidation.FailureCode(gate)
				projection.FailureCode = &failure
			}
		}
		result = append(result, projection)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode RPG gate projection: %w", err)
	}
	return string(encoded), nil
}

func indexOfGate(gate rpgvalidation.Gate) int {
	for index, candidate := range rpgvalidation.GateOrder() {
		if candidate == gate {
			return index
		}
	}
	return -1
}

func isPositionGate(gate rpgvalidation.Gate) bool {
	return gate == rpgvalidation.GateInitialPosition || gate == rpgvalidation.GateSavePointRecorded ||
		gate == rpgvalidation.GatePostSaveStateDiverged || gate == rpgvalidation.GateRestorePosition ||
		gate == rpgvalidation.GateRestoreInput
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func strictEvidence(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode RPG validation evidence: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrProtocol
	}
	return nil
}

func canonicalEvidence(contents []byte, target any) (json.RawMessage, error) {
	if err := strictEvidence(contents, target); err != nil {
		return nil, ErrProtocol
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return nil, ErrProtocol
	}
	return encoded, nil
}

func emptyEvidence(contents []byte) (json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := strictEvidence(contents, &value); err != nil || len(value) != 0 {
		return nil, ErrProtocol
	}
	return json.RawMessage(`{}`), nil
}

func (service *Service) validateGateEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	runtime validationRuntime,
	request GateRequest,
) (json.RawMessage, *rpgvalidation.Position, error) {
	gate, phase := rpgvalidation.Gate(request.Gate), rpgvalidation.Phase(request.Phase)
	if !validGateRequest(gate, phase, request) {
		return nil, nil, ErrProtocol
	}
	if phase != rpgvalidation.PhasePass {
		evidence, err := emptyEvidence(request.Evidence)
		return evidence, nil, err
	}
	if isPositionGate(gate) {
		var position rpgvalidation.Position
		evidence, err := canonicalEvidence(request.Evidence, &position)
		return evidence, &position, err
	}
	switch gate {
	case rpgvalidation.GateEngineProfile:
		return validateEngineProfileEvidence(runtime, request.Evidence)
	case rpgvalidation.GateFrames300:
		return validateFrameEvidence(request.Evidence)
	case rpgvalidation.GateInput, rpgvalidation.GateAudio:
		return validateObservedEvidence(request.Evidence)
	case rpgvalidation.GateCheckpointCreated:
		return service.validateCheckpointEvidence(ctx, transaction, runtime.id, request.Evidence)
	case rpgvalidation.GateRuntimeReady, rpgvalidation.GateOriginalLaunchEnded,
		rpgvalidation.GateRestoreStarted, rpgvalidation.GateRestoreScreenshot:
		return validateEmptyGateEvidence(gate, runtime, request.Evidence)
	case rpgvalidation.GateInitialPosition, rpgvalidation.GateSavePointRecorded,
		rpgvalidation.GatePostSaveStateDiverged, rpgvalidation.GateRestorePosition,
		rpgvalidation.GateRestoreInput:
		return nil, nil, ErrProtocol
	default:
		return nil, nil, ErrProtocol
	}
}

func validGateRequest(gate rpgvalidation.Gate, phase rpgvalidation.Phase, request GateRequest) bool {
	validPhase := phase == rpgvalidation.PhaseBegin || phase == rpgvalidation.PhasePass ||
		phase == rpgvalidation.PhaseFail
	return indexOfGate(gate) >= 0 && validPhase && validCanonicalUUID(request.EventID) &&
		request.Sequence >= 1 && request.ObservedAtMS > 0 && len(request.Evidence) > 0
}

func validateEngineProfileEvidence(
	runtime validationRuntime,
	contents json.RawMessage,
) (json.RawMessage, *rpgvalidation.Position, error) {
	var evidence struct {
		Generation    string `json:"generation"`
		AdapterID     string `json:"adapterId"`
		EngineProfile string `json:"engineProfile"`
	}
	canonical, err := canonicalEvidence(contents, &evidence)
	if err != nil || evidence.Generation != runtime.generation || evidence.AdapterID != runtime.adapterID ||
		evidence.EngineProfile != engineProfile(runtime.generation) {
		return nil, nil, ErrProtocol
	}
	return canonical, nil, nil
}

func validateFrameEvidence(contents json.RawMessage) (json.RawMessage, *rpgvalidation.Position, error) {
	var evidence struct {
		ContinuousFrames int64 `json:"continuousFrames"`
	}
	canonical, err := canonicalEvidence(contents, &evidence)
	if err != nil || evidence.ContinuousFrames < 300 || evidence.ContinuousFrames > 36000 {
		return nil, nil, ErrProtocol
	}
	return canonical, nil, nil
}

func validateObservedEvidence(contents json.RawMessage) (json.RawMessage, *rpgvalidation.Position, error) {
	var evidence struct {
		Observed bool `json:"observed"`
	}
	canonical, err := canonicalEvidence(contents, &evidence)
	if err != nil || !evidence.Observed {
		return nil, nil, ErrProtocol
	}
	return canonical, nil, nil
}

func (service *Service) validateCheckpointEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	validationID string,
	contents json.RawMessage,
) (json.RawMessage, *rpgvalidation.Position, error) {
	var evidence struct {
		PayloadKind string `json:"payloadKind"`
		SizeBytes   int64  `json:"sizeBytes"`
		SHA256      string `json:"sha256"`
	}
	canonical, err := canonicalEvidence(contents, &evidence)
	if err != nil {
		return nil, nil, ErrProtocol
	}
	var payloadKind, digest string
	var size int64
	if err := transaction.QueryRowContext(ctx, `
SELECT payload_kind,size_bytes,payload_sha256
FROM rpgmaker_runtime_validation_checkpoints WHERE validation_id=?
`, validationID).Scan(&payloadKind, &size, &digest); err != nil ||
		evidence.PayloadKind != payloadKind || evidence.SizeBytes != size || evidence.SHA256 != digest {
		return nil, nil, ErrProtocol
	}
	return canonical, nil, nil
}

func validateEmptyGateEvidence(
	gate rpgvalidation.Gate,
	runtime validationRuntime,
	contents json.RawMessage,
) (json.RawMessage, *rpgvalidation.Position, error) {
	canonical, err := emptyEvidence(contents)
	if err != nil {
		return nil, nil, err
	}
	if gate == rpgvalidation.GateRestoreScreenshot && !runtime.evidenceScreenshot.Valid {
		return nil, nil, ErrProtocol
	}
	return canonical, nil, nil
}

func engineProfile(generation string) string {
	return map[string]string{
		"RPG2000": "rpg2k", "RPG2003": "rpg2k3", "RPGXP": "rgss1", "RPGVX": "rgss2",
		"RPGVXACE": "rgss3", "RPGMV": "RPGMV", "RPGMZ": "RPGMZ",
	}[generation]
}
