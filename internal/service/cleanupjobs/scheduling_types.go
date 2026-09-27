package cleanupjobs

import (
	"context"
	"errors"
)

type ScopeType string

const (
	ScopeImportItem        ScopeType = "IMPORT_ITEM"
	ScopeImportJob         ScopeType = "IMPORT_JOB"
	ScopeSourceImportItem  ScopeType = "SOURCE_IMPORT_ITEM"
	ScopeUploadConsumption ScopeType = "UPLOAD_CONSUMPTION"
	ScopeGame              ScopeType = "GAME"
	ScopePath              ScopeType = "STORAGE_PATH"
)

type Reason string

const (
	ReasonImportPublished Reason = "IMPORT_PUBLISHED"
	ReasonImportDiscarded Reason = "IMPORT_DISCARDED"
	ReasonImportFailed    Reason = "IMPORT_FAILED_FINAL"
	ReasonImportCancelled Reason = "IMPORT_CANCELLED"
	ReasonImportTerminal  Reason = "IMPORT_JOB_TERMINAL"
	ReasonSourceTerminal  Reason = "SOURCE_TERMINAL"
	ReasonUploadConsumed  Reason = "UPLOAD_CONSUMED"
	ReasonGameDeleted     Reason = "GAME_DELETED"
)

var (
	ErrScopeInvalid      = errors.New("OWNER_CLEANUP_SCOPE_INVALID")
	ErrScheduleIDInvalid = errors.New("OWNER_CLEANUP_SCHEDULE_ID_INVALID")
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
	RelativePath string `json:"relativePath,omitempty"`
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

// Scheduling capabilities belong to the transaction that changes the owner.
// Domain bindings expose only the reads and writes required by their lifecycle.
type JobWriter interface {
	CreateJob(context.Context, ScheduledJob) error
}

type OwnerSchedulingScope interface {
	JobWriter
	Owner(context.Context, Scope) (Owner, error)
	BeginRelease(context.Context, OwnerRelease) error
}

type ItemSchedulingScope interface {
	OwnerSchedulingScope
	PendingChildren(context.Context, string) (int64, error)
}

type ConsumptionSchedulingScope interface {
	JobWriter
	Consumption(context.Context, string) (Consumption, error)
}
