package runtimevalidation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	rpgvalidation "retrom/internal/rpgmaker/validation"
)

type viewRow struct {
	View
	machineJSON                          string
	evidenceGeneration                   sql.NullString
	launchID, restoreID                  sql.NullString
	failureCode, decisionNote, decidedBy sql.NullString
	decidedAt                            sql.NullInt64
	screenshotBlobID                     sql.NullString
}

func (service *Service) Get(ctx context.Context, importItemID, validationID string) (View, error) {
	if !validCanonicalUUID(importItemID) || !validCanonicalUUID(validationID) {
		return View{}, ErrNotFound
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return View{}, fmt.Errorf("begin RPG validation view: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if err := expireOneValidation(ctx, transaction, validationID, now); err != nil {
		return View{}, err
	}
	if err := reconcileClosedValidationWindows(ctx, transaction, importItemID, now); err != nil {
		return View{}, err
	}
	view, err := loadView(ctx, transaction, importItemID, validationID)
	if err != nil {
		return View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return View{}, fmt.Errorf("commit RPG validation view: %w", err)
	}
	return view, nil
}

func loadView(ctx context.Context, transaction *sql.Tx, importItemID, validationID string) (View, error) {
	var row viewRow
	err := transaction.QueryRowContext(ctx, `
SELECT id,import_item_id,review_version_at_create,runtime_binding_revision,
 effective_source_snapshot_id,generation,evidence_generation,evidence_confidence,
 provider_id,target_id,game_compatibility_line,target_contract_sha256,
 dependency_snapshot_sha256,project_fingerprint,launch_id,restore_launch_id,state,
 last_gate_sequence,machine_gates_json,failure_code,decision_note,decided_by_user_id,
 decided_at_ms,evidence_screenshot_blob_id,created_at_ms,updated_at_ms,expires_at_ms
FROM rpgmaker_runtime_validations WHERE id=? AND import_item_id=?
`, validationID, importItemID).Scan(
		&row.ValidationID, &row.ImportItemID, &row.ReviewVersionAtCreate, &row.RuntimeBindingVersion,
		&row.RouteEvidence.EffectiveSourceSnapshotID, &row.RouteEvidence.Generation,
		&row.evidenceGeneration, &row.RouteEvidence.EvidenceConfidence, &row.RouteEvidence.ProviderID,
		&row.RouteEvidence.TargetID, &row.RouteEvidence.GameCompatibilityLine,
		&row.RouteEvidence.TargetContractSHA256, &row.RouteEvidence.DependencySnapshotSHA256,
		&row.RouteEvidence.ProjectFingerprint, &row.launchID, &row.restoreID, &row.State,
		&row.LastGateSequence, &row.machineJSON, &row.failureCode, &row.decisionNote, &row.decidedBy,
		&row.decidedAt, &row.screenshotBlobID, &row.CreatedAtMS, &row.UpdatedAtMS, &row.ExpiresAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, ErrNotFound
	}
	if err != nil {
		return View{}, fmt.Errorf("load RPG validation view: %w", err)
	}
	if !row.launchID.Valid && row.State != string(rpgvalidation.StateFailed) {
		return View{}, ErrInvalidState
	}
	row.LaunchID = stringPointer(row.launchID)
	row.RouteEvidence.EvidenceGeneration = stringPointer(row.evidenceGeneration)
	row.RestoreLaunchID = stringPointer(row.restoreID)
	row.FailureCode = stringPointer(row.failureCode)
	if err := json.Unmarshal([]byte(row.machineJSON), &row.MachineGates); err != nil ||
		len(row.MachineGates) != len(rpgvalidation.GateOrder()) {
		return View{}, ErrProtocol
	}
	checkpoint, err := loadCheckpointRoundTrip(ctx, transaction, row)
	if err != nil {
		return View{}, err
	}
	row.CheckpointRoundTrip = checkpoint
	if row.decidedBy.Valid && row.decidedAt.Valid {
		decision := "FAIL"
		if row.State == string(rpgvalidation.StatePassed) {
			decision = "PASS"
		}
		row.Decision = &ReviewerDecision{
			Decision: decision, Note: row.decisionNote.String, DecidedAt: row.decidedAt.Int64,
		}
	}
	return row.View, nil
}

func loadCheckpointRoundTrip(
	ctx context.Context,
	transaction *sql.Tx,
	row viewRow,
) (CheckpointRoundTrip, error) {
	result := CheckpointRoundTrip{
		OriginalLaunchID: stringPointer(row.launchID), RestoreLaunchID: stringPointer(row.restoreID),
	}
	var checkpointFormat, digest string
	var size int64
	err := transaction.QueryRowContext(ctx, `
SELECT checkpoint_format,size_bytes,payload_sha256
FROM rpgmaker_runtime_validation_checkpoints WHERE validation_id=?
`, row.ValidationID).Scan(&checkpointFormat, &size, &digest)
	if err == nil {
		result.Created = true
		result.CheckpointFormat, result.SizeBytes, result.SHA256 = &checkpointFormat, &size, &digest
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CheckpointRoundTrip{}, fmt.Errorf("load RPG checkpoint: %w", err)
	}
	events, err := loadGateEvents(ctx, transaction, row.ValidationID)
	if err != nil {
		return CheckpointRoundTrip{}, err
	}
	for _, event := range events {
		if event.Phase != rpgvalidation.PhasePass {
			continue
		}
		applyCheckpointEvent(&result, event, row)
	}
	if row.screenshotBlobID.Valid {
		url := "/api/v1/admin/review-assets/" + row.ValidationID
		result.ScreenshotURL = &url
	}
	return result, nil
}

func applyCheckpointEvent(result *CheckpointRoundTrip, event storedEvent, row viewRow) {
	position := projectPosition(event.Position)
	switch event.Gate {
	case rpgvalidation.GateInitialPosition:
		result.InitialPosition = position
	case rpgvalidation.GateSavePointRecorded:
		result.SavedPosition = position
	case rpgvalidation.GatePostSaveStateDiverged:
		result.DivergedPosition = position
	case rpgvalidation.GateOriginalLaunchEnded:
		result.OriginalLaunchEnded = true
	case rpgvalidation.GateRestoreStarted:
		result.RestoreStarted = true
	case rpgvalidation.GateRestorePosition:
		result.RestoredPosition = position
		result.PositionVerified = samePosition(result.SavedPosition, position) &&
			!samePosition(result.InitialPosition, position) &&
			!samePosition(result.DivergedPosition, position) && row.restoreID.Valid &&
			row.restoreID.String != row.launchID.String
	case rpgvalidation.GateRestoreInput:
		result.RestoreInputPosition = position
		result.RestoreInputVerified = result.PositionVerified &&
			!samePosition(result.RestoredPosition, position)
	case rpgvalidation.GateRuntimeReady, rpgvalidation.GateEngineProfile, rpgvalidation.GateFrames300,
		rpgvalidation.GateInput, rpgvalidation.GateAudio, rpgvalidation.GateCheckpointCreated,
		rpgvalidation.GateRestoreScreenshot:
		return
	}
}

func (service *Service) Decide(
	ctx context.Context,
	importItemID, validationID string,
	expectedReviewVersion int64,
	actorUserID, decision, note string,
) (View, error) {
	if !validDecisionRequest(importItemID, validationID, actorUserID, expectedReviewVersion) {
		return View{}, ErrDecision
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return View{}, fmt.Errorf("begin RPG validation decision: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if err := expireOneValidation(ctx, transaction, validationID, now); err != nil {
		return View{}, err
	}
	current, err := currentValidationBinding(ctx, transaction, importItemID, validationID, expectedReviewVersion)
	if err != nil {
		return View{}, err
	}
	machine := &rpgvalidation.Machine{State: rpgvalidation.State(current.state)}
	normalized, err := machine.Decide(rpgvalidation.Decision(decision), note)
	if err != nil {
		return View{}, ErrDecision
	}
	if decision == string(rpgvalidation.DecisionPass) && !current.bindingMatches {
		return View{}, ErrBindingStale
	}
	if err := persistValidationDecision(
		ctx, transaction, machine, normalized, actorUserID, now, validationID, importItemID,
	); err != nil {
		return View{}, err
	}
	view, err := loadView(ctx, transaction, importItemID, validationID)
	if err != nil {
		return View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return View{}, fmt.Errorf("commit RPG validation decision: %w", err)
	}
	return view, nil
}

func validDecisionRequest(importItemID, validationID, actorUserID string, expectedReviewVersion int64) bool {
	return validCanonicalUUID(importItemID) && validCanonicalUUID(validationID) &&
		validCanonicalUUID(actorUserID) && expectedReviewVersion >= 1
}

func persistValidationDecision(
	ctx context.Context,
	transaction *sql.Tx,
	machine *rpgvalidation.Machine,
	note, actorUserID string,
	now int64,
	validationID, importItemID string,
) error {
	failureCode := any(nil)
	if machine.FailureCode != "" {
		failureCode = machine.FailureCode
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET state=?,failure_code=?,decision_note=?,decided_by_user_id=?,decided_at_ms=?,updated_at_ms=?
WHERE id=? AND import_item_id=? AND state='AWAITING_DECISION'
`, string(machine.State), failureCode, note, actorUserID, now, now, validationID, importItemID)
	if err != nil {
		return fmt.Errorf("persist RPG validation decision: %w", err)
	}
	if count, rowsErr := result.RowsAffected(); rowsErr != nil || count != 1 {
		return ErrInvalidState
	}
	return nil
}

type currentValidation struct {
	state          string
	bindingMatches bool
}

func currentValidationBinding(
	ctx context.Context,
	transaction *sql.Tx,
	importItemID, validationID string,
	expectedReviewVersion int64,
) (currentValidation, error) {
	var result currentValidation
	var reviewVersion int64
	var matches int
	err := transaction.QueryRowContext(ctx, `
SELECT validation.state,draft.version,
	 CASE WHEN draft.runtime_binding_revision=validation.runtime_binding_revision
	  AND draft.effective_source_snapshot_id=validation.effective_source_snapshot_id
	  AND profile.project_fingerprint=validation.project_fingerprint
	  AND profile.generation=validation.generation
	  AND profile.evidence_generation IS validation.evidence_generation
	  AND profile.evidence_confidence=validation.evidence_confidence
	  AND profile.provider_id=validation.provider_id AND profile.target_id=validation.target_id
	  AND profile.game_compatibility_line=validation.game_compatibility_line
	  AND profile.target_contract_sha256=validation.target_contract_sha256
  AND profile.dependency_snapshot_sha256=validation.dependency_snapshot_sha256
 THEN 1 ELSE 0 END
FROM rpgmaker_runtime_validations validation
JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN import_items item ON item.id=draft.import_item_id AND item.state='REVIEW_PENDING'
WHERE validation.id=? AND validation.import_item_id=?
`, validationID, importItemID).Scan(&result.state, &reviewVersion, &matches)
	if errors.Is(err, sql.ErrNoRows) {
		return currentValidation{}, ErrNotFound
	}
	if err != nil {
		return currentValidation{}, fmt.Errorf("load current RPG validation: %w", err)
	}
	if reviewVersion != expectedReviewVersion {
		return currentValidation{}, ErrVersion
	}
	result.bindingMatches = matches == 1
	return result, nil
}

func (service *Service) CurrentForRestore(
	ctx context.Context,
	importItemID, validationID string,
	expectedReviewVersion int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin restore validation check: %w", err)
	}
	defer cleanup.Rollback(transaction)
	current, err := currentValidationBinding(
		ctx, transaction, importItemID, validationID, expectedReviewVersion,
	)
	if err != nil {
		return err
	}
	if current.state != string(rpgvalidation.StateCheckpointed) {
		return ErrInvalidState
	}
	if !current.bindingMatches {
		return ErrBindingStale
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit restore validation check: %w", err)
	}
	return nil
}

func (service *Service) AbortCreated(ctx context.Context, validationID, failureCode string) error {
	now := service.now().UnixMilli()
	_, err := service.database.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET state='FAILED',failure_code=?,updated_at_ms=?
WHERE id=? AND state='CREATED'
`, failureCode, now, validationID)
	if err != nil {
		return fmt.Errorf("abort RPG validation: %w", err)
	}
	return nil
}

func expireOneValidation(ctx context.Context, transaction *sql.Tx, validationID string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET state='EXPIRED',failure_code='RPG_RUNTIME_TIMEOUT',updated_at_ms=?
WHERE id=? AND expires_at_ms<=? AND state NOT IN ('PASSED','FAILED','EXPIRED')
`, now, validationID, now); err != nil {
		return fmt.Errorf("expire RPG validation: %w", err)
	}
	return nil
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copyValue := value.String
	return &copyValue
}

func projectPosition(value *rpgvalidation.Position) *Position {
	if value == nil {
		return nil
	}
	return &Position{
		MapID: value.MapID, PlayerX: value.PlayerX, PlayerY: value.PlayerY, FixtureState: value.FixtureState,
	}
}

func samePosition(left, right *Position) bool {
	return left != nil && right != nil && *left == *right
}
