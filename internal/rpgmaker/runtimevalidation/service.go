package runtimevalidation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	rpgvalidation "retrom/internal/rpgmaker/validation"
)

var (
	ErrNotFound     = errors.New("RPG_RUNTIME_VALIDATION_NOT_FOUND")
	ErrVersion      = errors.New("RPG_RUNTIME_VALIDATION_VERSION_CONFLICT")
	ErrInvalidState = errors.New("RPG_RUNTIME_INVALID_STATE")
	ErrProtocol     = errors.New("RPG_RUNTIME_PROTOCOL_VIOLATION")
	ErrBindingStale = errors.New("RPG_RUNTIME_CONTENT_MISMATCH")
	ErrCredential   = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrImageInvalid = errors.New("RPG_RUNTIME_SCREENSHOT_INVALID")
	ErrDecision     = errors.New("RPG_RUNTIME_VALIDATION_DECISION_INVALID")
)

const validationLifetime = 15 * time.Minute

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	now      func() time.Time
}

func New(database *sql.DB, blobs *blobstore.Store, now func() time.Time) *Service {
	return &Service{database: database, blobs: blobs, now: now}
}

type Binding struct {
	ValidationID  string
	ImportItemID  string
	ReviewVersion int64
	ExpiresAtMS   int64
}

type frozenBinding struct {
	reviewDraftID, sourceSnapshotID, projectFingerprint string
	generation, providerID, targetID                    string
	dependencySnapshotSHA256, evidenceConfidence        string
	evidenceGeneration                                  sql.NullString
	reviewVersion                                       int64
}

func (service *Service) Create(
	ctx context.Context,
	importItemID string,
	expectedReviewVersion int64,
) (Binding, error) {
	if !validCanonicalUUID(importItemID) || expectedReviewVersion < 1 {
		return Binding{}, ErrNotFound
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Binding{}, fmt.Errorf("begin RPG validation: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if err := expireValidation(ctx, transaction, importItemID, now); err != nil {
		return Binding{}, err
	}
	if err := reconcileClosedValidationWindows(ctx, transaction, importItemID, now); err != nil {
		return Binding{}, err
	}
	binding, err := loadCurrentBinding(ctx, transaction, importItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	if err != nil {
		return Binding{}, fmt.Errorf("load RPG validation binding: %w", err)
	}
	if binding.reviewVersion != expectedReviewVersion {
		return Binding{}, ErrVersion
	}
	if exists, err := activeValidationExists(ctx, transaction, importItemID); err != nil {
		return Binding{}, err
	} else if exists {
		return Binding{}, ErrInvalidState
	}
	validationID, err := uuid.NewV7()
	if err != nil {
		return Binding{}, fmt.Errorf("create RPG validation id: %w", err)
	}
	expiresAt := now + int64(validationLifetime/time.Millisecond)
	machineGates, err := initialMachineGates()
	if err != nil {
		return Binding{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,effective_source_snapshot_id,project_fingerprint,generation,
 evidence_generation,evidence_confidence,provider_id,target_id,
 dependency_snapshot_sha256,
 state,last_gate_sequence,machine_gates_json,created_at_ms,updated_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,
 'CREATED',0,?,?,?,?)
`, validationID.String(), importItemID, binding.reviewVersion, binding.sourceSnapshotID,
		binding.projectFingerprint, binding.generation,
		nullableString(binding.evidenceGeneration), binding.evidenceConfidence, binding.providerID, binding.targetID,
		binding.dependencySnapshotSHA256,
		machineGates, now, now, expiresAt); err != nil {
		return Binding{}, fmt.Errorf("insert RPG validation: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Binding{}, fmt.Errorf("commit RPG validation: %w", err)
	}
	return Binding{
		ValidationID: validationID.String(), ImportItemID: importItemID,
		ReviewVersion: binding.reviewVersion,
		ExpiresAtMS:   expiresAt,
	}, nil
}

func loadCurrentBinding(ctx context.Context, transaction *sql.Tx, importItemID string) (frozenBinding, error) {
	var binding frozenBinding
	var itemState, contentKind string
	err := transaction.QueryRowContext(ctx, `
SELECT draft.id,draft.version,draft.effective_source_snapshot_id,
 profile.project_fingerprint,profile.generation,profile.evidence_generation,
 profile.evidence_confidence,profile.provider_id,profile.target_id,profile.dependency_snapshot_sha256,
 item.state,snapshot.content_kind
FROM review_drafts draft
JOIN import_items item ON item.id=draft.import_item_id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
 AND snapshot.import_item_id=draft.import_item_id
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN runtime_targets target ON target.provider_id=profile.provider_id AND target.target_id=profile.target_id
WHERE draft.import_item_id=?
`, importItemID).Scan(
		&binding.reviewDraftID, &binding.reviewVersion, &binding.sourceSnapshotID,
		&binding.projectFingerprint, &binding.generation,
		&binding.evidenceGeneration, &binding.evidenceConfidence, &binding.providerID, &binding.targetID,
		&binding.dependencySnapshotSHA256, &itemState, &contentKind,
	)
	if err != nil {
		return frozenBinding{}, fmt.Errorf("load frozen RPG validation binding: %w", err)
	}
	if itemState != "REVIEW_PENDING" || contentKind != "RPG_MAKER_PROJECT" {
		return frozenBinding{}, sql.ErrNoRows
	}
	return binding, nil
}

func activeValidationExists(
	ctx context.Context,
	transaction *sql.Tx,
	importItemID string,
) (bool, error) {
	var count int
	err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM rpgmaker_runtime_validations
WHERE import_item_id=?
 AND state NOT IN ('PASSED','FAILED','EXPIRED')
`, importItemID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query active RPG validation: %w", err)
	}
	return count != 0, nil
}

func expireValidation(ctx context.Context, transaction *sql.Tx, importItemID string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='EXPIRED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE purpose='RPG_RUNTIME_VALIDATION' AND state IN ('CREATED','ACTIVE')
 AND rpgmaker_runtime_validation_id IN (
  SELECT id FROM rpgmaker_runtime_validations
  WHERE import_item_id=? AND expires_at_ms<=?
   AND state NOT IN ('PASSED','FAILED')
 )
`, now, now, importItemID, now); err != nil {
		return fmt.Errorf("expire RPG validation launches: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET state='EXPIRED',failure_code='RPG_RUNTIME_TIMEOUT',updated_at_ms=?
WHERE import_item_id=? AND expires_at_ms<=?
 AND state NOT IN ('PASSED','FAILED','EXPIRED')
`, now, importItemID, now); err != nil {
		return fmt.Errorf("expire RPG validation: %w", err)
	}
	return nil
}

func reconcileClosedValidationWindows(
	ctx context.Context,
	transaction *sql.Tx,
	importItemID string,
	now int64,
) error {
	_, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET state='FAILED',failure_code='RPG_RUNTIME_VALIDATION_WINDOW_CLOSED',updated_at_ms=?
WHERE import_item_id=? AND state NOT IN ('PASSED','FAILED','EXPIRED')
AND (
  launch_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM launch_sessions launch
    WHERE launch.id=rpgmaker_runtime_validations.launch_id
      AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
  ) AND NOT EXISTS(
    SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=rpgmaker_runtime_validations.id
      AND event.gate='ORIGINAL_LAUNCH_ENDED' AND event.phase='PASS'
  )
  OR restore_launch_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM launch_sessions launch
    WHERE launch.id=rpgmaker_runtime_validations.restore_launch_id
      AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
  ) AND NOT EXISTS(
    SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=rpgmaker_runtime_validations.id
      AND event.gate='RESTORE_INPUT' AND event.phase='PASS'
  )
)
`, now, importItemID)
	if err != nil {
		return fmt.Errorf("reconcile closed RPG validation window: %w", err)
	}
	return nil
}

func initialMachineGates() (string, error) {
	items := make([]MachineGate, 0, len(rpgvalidation.GateOrder()))
	for _, gate := range rpgvalidation.GateOrder() {
		items = append(items, MachineGate{Gate: string(gate), Status: GateNotStarted})
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("encode RPG validation gates: %w", err)
	}
	return string(encoded), nil
}

func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func validCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}
