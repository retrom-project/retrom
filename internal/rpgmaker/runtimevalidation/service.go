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
	ValidationID          string
	ImportItemID          string
	ReviewVersion         int64
	RuntimeBindingVersion int64
	ExpiresAtMS           int64
}

type frozenBinding struct {
	reviewDraftID, sourceSnapshotID, projectFingerprint                 string
	coreID, generation, routeKey, artifactID, artifactSetSHA256         string
	adapterID, adapterABI, dependencySnapshotSHA256, evidenceConfidence string
	evidenceGeneration                                                  sql.NullString
	reviewVersion, runtimeBindingRevision                               int64
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
	if exists, err := activeValidationExists(ctx, transaction, importItemID, binding.runtimeBindingRevision); err != nil {
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
 id,import_item_id,review_version_at_create,runtime_binding_revision,
 effective_source_snapshot_id,project_fingerprint,core_id,generation,
 evidence_generation,evidence_confidence,route_key,artifact_id,
 artifact_set_sha256,adapter_id,adapter_abi,dependency_snapshot_sha256,
 state,last_gate_sequence,machine_gates_json,created_at_ms,updated_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,
 'CREATED',0,?,?,?,?)
`, validationID.String(), importItemID, binding.reviewVersion, binding.runtimeBindingRevision,
		binding.sourceSnapshotID, binding.projectFingerprint, binding.coreID, binding.generation,
		nullableString(binding.evidenceGeneration), binding.evidenceConfidence, binding.routeKey, binding.artifactID,
		binding.artifactSetSHA256, binding.adapterID, binding.adapterABI, binding.dependencySnapshotSHA256,
		machineGates, now, now, expiresAt); err != nil {
		return Binding{}, fmt.Errorf("insert RPG validation: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Binding{}, fmt.Errorf("commit RPG validation: %w", err)
	}
	return Binding{
		ValidationID: validationID.String(), ImportItemID: importItemID,
		ReviewVersion: binding.reviewVersion, RuntimeBindingVersion: binding.runtimeBindingRevision,
		ExpiresAtMS: expiresAt,
	}, nil
}

func loadCurrentBinding(ctx context.Context, transaction *sql.Tx, importItemID string) (frozenBinding, error) {
	var binding frozenBinding
	var itemState, contentKind string
	err := transaction.QueryRowContext(ctx, `
SELECT draft.id,draft.version,draft.runtime_binding_revision,draft.effective_source_snapshot_id,
 profile.project_fingerprint,profile.selected_core_id,profile.generation,profile.evidence_generation,
 profile.evidence_confidence,profile.route_key,profile.artifact_id,profile.artifact_set_sha256,
 profile.adapter_id,profile.adapter_abi,profile.dependency_snapshot_sha256,
 item.state,snapshot.content_kind
FROM review_drafts draft
JOIN import_items item ON item.id=draft.import_item_id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
 AND snapshot.import_item_id=draft.import_item_id
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN core_artifacts artifact ON artifact.id=profile.artifact_id
 AND artifact.core_id=profile.selected_core_id AND artifact.route_key=profile.route_key
 AND artifact.runtime_family='RPGMAKER'
 AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
WHERE draft.import_item_id=?
`, importItemID).Scan(
		&binding.reviewDraftID, &binding.reviewVersion, &binding.runtimeBindingRevision,
		&binding.sourceSnapshotID, &binding.projectFingerprint, &binding.coreID, &binding.generation,
		&binding.evidenceGeneration, &binding.evidenceConfidence, &binding.routeKey, &binding.artifactID,
		&binding.artifactSetSHA256, &binding.adapterID, &binding.adapterABI,
		&binding.dependencySnapshotSHA256, &itemState, &contentKind,
	)
	if err != nil {
		return frozenBinding{}, fmt.Errorf("load frozen RPG validation binding: %w", err)
	}
	if itemState != "REVIEW_PENDING" || contentKind != "RPG_MAKER_PROJECT_V1" {
		return frozenBinding{}, sql.ErrNoRows
	}
	return binding, nil
}

func activeValidationExists(
	ctx context.Context,
	transaction *sql.Tx,
	importItemID string,
	bindingRevision int64,
) (bool, error) {
	var count int
	err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM rpgmaker_runtime_validations
WHERE import_item_id=? AND runtime_binding_revision=?
 AND state NOT IN ('PASSED','FAILED','EXPIRED')
`, importItemID, bindingRevision).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query active RPG validation: %w", err)
	}
	return count != 0, nil
}

func expireValidation(ctx context.Context, transaction *sql.Tx, importItemID string, now int64) error {
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
