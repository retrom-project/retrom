package payloadrelease

import (
	"context"
	"errors"
)

type ScopeType string

const (
	ScopeImportItem                 ScopeType = "IMPORT_ITEM"
	ScopeImportJob                  ScopeType = "IMPORT_JOB"
	ScopePegasusImportItem          ScopeType = "PEGASUS_IMPORT_ITEM"
	ScopeEmulationStationImportItem ScopeType = "EMULATIONSTATION_IMPORT_ITEM"
	ScopeUploadConsumption          ScopeType = "UPLOAD_CONSUMPTION"
	ScopeGame                       ScopeType = "GAME"
	ScopeBlob                       ScopeType = "BLOB"
)

type Reason string

const (
	ReasonImportPublished          Reason = "IMPORT_PUBLISHED"
	ReasonImportDiscarded          Reason = "IMPORT_DISCARDED"
	ReasonImportFailed             Reason = "IMPORT_FAILED_FINAL"
	ReasonImportCancelled          Reason = "IMPORT_CANCELLED"
	ReasonImportTerminal           Reason = "IMPORT_JOB_TERMINAL"
	ReasonPegasusTerminal          Reason = "PEGASUS_TERMINAL"
	ReasonEmulationStationTerminal Reason = "EMULATIONSTATION_TERMINAL"
	ReasonUploadConsumed           Reason = "UPLOAD_CONSUMED"
	ReasonGameDeleted              Reason = "GAME_DELETED"
)

var (
	ErrScopeInvalid      = errors.New("PAYLOAD_RELEASE_SCOPE_INVALID")
	ErrScheduleIDInvalid = errors.New("PAYLOAD_RELEASE_SCHEDULE_ID_INVALID")
)

type Input struct {
	SchemaVersion int         `json:"schemaVersion"`
	Kind          string      `json:"kind"`
	Scope         Scope       `json:"scope"`
	ExecutionID   string      `json:"executionId"`
	Inputs        ScopeInputs `json:"inputs"`
}

type Scope struct {
	Type ScopeType `json:"type"`
	ID   string    `json:"id"`
}

type ScopeInputs struct {
	ScopeVersion int64  `json:"scopeVersion,omitempty"`
	Reason       Reason `json:"reason,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
}

type ScheduleRequest struct {
	Scope        Scope
	ScopeVersion int64
	Reason       Reason
	NowMS        int64
}

type Owner struct {
	Scope                                       Scope
	State, PayloadState, ReleaseJobID, PublicID string
	Version                                     int64
	Retryable                                   bool
}

type Consumption struct {
	Version       int64
	Released      bool
	ExistingJobID string
}

type ScheduledJob struct {
	ID, DedupeKey, InputJSON, InputDigest string
	Scope                                 Scope
	NowMS                                 int64
}

type OwnerRelease struct {
	Before     Owner
	JobID      string
	NowMS      int64
	DeleteGame bool
}

// SchedulingScope belongs to the transaction that makes the owner terminal.
// Scheduled identities become durable only when that complete transaction commits.
type SchedulingScope interface {
	Owner(context.Context, Scope) (Owner, error)
	PendingChildren(context.Context, string) (int64, error)
	Consumption(context.Context, string) (Consumption, error)
	CreateJob(context.Context, ScheduledJob) error
	BeginRelease(context.Context, OwnerRelease) error
}
