package payloadrelease

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

type ScopeType string

const (
	ScopeImportItem        ScopeType = "IMPORT_ITEM"
	ScopeImportJob         ScopeType = "IMPORT_JOB"
	ScopePegasusImportItem ScopeType = "PEGASUS_IMPORT_ITEM"
	ScopeUploadConsumption ScopeType = "UPLOAD_CONSUMPTION"
	ScopeGame              ScopeType = "GAME"
	ScopeBlob              ScopeType = "BLOB"
)

type Reason string

const (
	ReasonImportPublished Reason = "IMPORT_PUBLISHED"
	ReasonImportDiscarded Reason = "IMPORT_DISCARDED"
	ReasonImportFailed    Reason = "IMPORT_FAILED_FINAL"
	ReasonImportCancelled Reason = "IMPORT_CANCELLED"
	ReasonImportTerminal  Reason = "IMPORT_JOB_TERMINAL"
	ReasonPegasusTerminal Reason = "PEGASUS_TERMINAL"
	ReasonUploadConsumed  Reason = "UPLOAD_CONSUMED"
	ReasonGameDeleted     Reason = "GAME_DELETED"
)

var (
	ErrScopeInvalid      = errors.New("PAYLOAD_RELEASE_SCOPE_INVALID")
	errScheduleIDInvalid = errors.New("PAYLOAD_RELEASE_SCHEDULE_ID_INVALID")
)

type scheduleInput struct {
	SchemaVersion int           `json:"schemaVersion"`
	Kind          string        `json:"kind"`
	Scope         scheduleScope `json:"scope"`
	ExecutionID   string        `json:"executionId"`
	Inputs        scopeInputs   `json:"inputs"`
}

type scheduleScope struct {
	Type ScopeType `json:"type"`
	ID   string    `json:"id"`
}

type scopeInputs struct {
	ScopeVersion int64  `json:"scopeVersion,omitempty"`
	Reason       Reason `json:"reason,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
}

func Schedule(
	ctx context.Context,
	transaction *sql.Tx,
	scopeType ScopeType,
	scopeID string,
	scopeVersion int64,
	reason Reason,
	now int64,
) (string, error) {
	if !validScope(scopeType) || scopeID == "" || scopeVersion < 1 || !validReason(reason) {
		return "", ErrScopeInvalid
	}
	jobID, idErr := uuid.NewV7()
	executionID, executionErr := uuid.NewV7()
	if idErr != nil || executionErr != nil {
		return "", errScheduleIDInvalid
	}
	input := scheduleInput{
		SchemaVersion: 1,
		Kind:          "PAYLOAD_RELEASE",
		Scope:         scheduleScope{Type: scopeType, ID: scopeID},
		ExecutionID:   executionID.String(),
		Inputs:        scopeInputs{ScopeVersion: scopeVersion, Reason: reason},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("payloadrelease/schedule input: %w", err)
	}
	inputDigest := sha256.Sum256(inputJSON)
	dedupeInput := "retrom-job-dedupe-v1\x00PAYLOAD_RELEASE\x00" + string(scopeType) + "\x00" + scopeID
	dedupeDigest := sha256.Sum256([]byte(dedupeInput))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,'PAYLOAD_RELEASE',?,1,'{"inputExecutionNo":1}',0,'QUEUED',0,4,1,?,?,?)
`, jobID.String(), scopeType, scopeID, hex.EncodeToString(dedupeDigest[:]), now, now, now); err != nil {
		return "", fmt.Errorf("payloadrelease/schedule job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return "", fmt.Errorf("payloadrelease/schedule input snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,?,?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)
`, jobID.String(), scopeType, scopeID, now); err != nil {
		return "", fmt.Errorf("payloadrelease/schedule event: %w", err)
	}
	return jobID.String(), nil
}

func validScope(value ScopeType) bool {
	switch value {
	case ScopeImportItem, ScopeImportJob, ScopePegasusImportItem, ScopeUploadConsumption, ScopeGame:
		return true
	case ScopeBlob:
		return false
	default:
		return false
	}
}

func validReason(value Reason) bool {
	switch value {
	case ReasonImportPublished, ReasonImportDiscarded, ReasonImportFailed, ReasonImportCancelled,
		ReasonImportTerminal, ReasonPegasusTerminal, ReasonUploadConsumed, ReasonGameDeleted:
		return true
	default:
		return false
	}
}
