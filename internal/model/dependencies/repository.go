package dependencies

import (
	"context"
	"errors"

	"retrom/internal/capability/format/arcadedat"
	"retrom/internal/capability/security/authn"
	"retrom/internal/model/datindex"
)

var ErrDATJobNotClaimed = errors.New("DEPENDENCY_DAT_JOB_NOT_CLAIMABLE")

//nolint:interfacebloat // named commands replace former CommitWrite callback
type Repository interface {
	TargetExists(context.Context, RuntimeTarget) (bool, error)
	FindDAT(context.Context, DATLookup) (DATState, error)
	CommitBootstrapDefinitions(context.Context, BootstrapCommand) error
	CommitEnsureDATJob(context.Context, EnsureDATJobCommand) (string, error)
	CommitClaimDAT(context.Context, ClaimDATCommand) error
	CommitFailDAT(context.Context, FailDATCommand) error
	CommitActivateDAT(context.Context, ActivateDATCommand) error
	CommitPublishDAT(context.Context, PublishDATCommand) error
}

// BootstrapCommand carries pre-computed BIOS requirements and DAT registration
// entries for the dependency bootstrap operation.
type BootstrapCommand struct {
	BIOSEntries []BIOSRequirement
	DATEntries  []DATBootstrapEntry
	NowMS       int64
}

// DATBootstrapEntry describes a single DAT version to register/reset/retire.
type DATBootstrapEntry struct {
	Registration DATRegistration
	Expected     CatalogStats
	Preferred    bool
}

// EnsureDATJobCommand finds an existing DAT job or creates a new one.
type EnsureDATJobCommand struct {
	DATID, DATSHA, ParserVersion string
	DedupeKey                    string
	JobID, ExecutionID           string
	NowMS                        int64
}

// ClaimDATCommand claims a DAT job and marks the DAT as parsing.
type ClaimDATCommand struct {
	Claim   JobClaim
	MarkDAT string
}

// FailDATCommand marks a DAT as failed and finishes its job.
type FailDATCommand struct {
	DATID  string
	Finish JobFinish
}

// ActivateDATCommand activates a DAT and syncs its requirements.
type ActivateDATCommand struct {
	DATID   string
	AuditID string
	NowMS   int64
}

// PublishDATCommand publishes a DAT catalog, activates it, and finishes the job.
type PublishDATCommand struct {
	Publication CatalogPublication
	Activation  ActivateDATCommand
	Finish      JobFinish
}

// WriteScope is used internally by the repo layer for transaction-bound operations.
type WriteScope struct {
	Targets      TargetRecords
	BIOS         BIOSRecords
	DAT          DATRecords
	Catalog      CatalogRecords
	Jobs         JobRecords
	Requirements datindex.Records
}
type TargetRecords interface {
	Exists(context.Context, RuntimeTarget) (bool, error)
}
type BIOSRecords interface {
	Upsert(context.Context, BIOSRequirement) error
}
type DATRecords interface {
	Register(context.Context, DATRegistration) (RegisteredDAT, error)
	Reset(context.Context, string, int64) error
	Retire(context.Context, RuntimeTarget, string, int64) error
	Activation(context.Context, string) (ActivationState, error)
	Select(context.Context, DATSelection) error
}
type CatalogRecords interface {
	Version(context.Context, string) (int64, error)
	Publish(context.Context, CatalogPublication) error
	MarkParsing(context.Context, string, int64) error
	MarkFailed(context.Context, string, int64) error
}
type JobRecords interface {
	Find(context.Context, string) (Job, bool, error)
	Create(context.Context, JobCreation) error
	Requeue(context.Context, string, int64) error
	Claim(context.Context, JobClaim) error
	Finish(context.Context, JobFinish) error
}
type (
	RuntimeTarget struct{ ProviderID, TargetID string }
	DATLookup     struct {
		CoreID string
		Target RuntimeTarget
		SHA256 string
	}
)

type DATState struct {
	ID, ParseStatus string
	Indexed         int64
}
type CatalogStats struct {
	MachineCount, ROMEntryCount, DiskEntryCount, BIOSSetCount, DefaultBIOSSetCount                    int64
	ExplicitBIOSMachineCount, BaseDependencyTargetCount, UnresolvedCloneofCount, UnresolvedRomofCount int64
}
type DATRegistration struct {
	CoreID               string
	Target               RuntimeTarget
	RelativePath, SHA256 string
	AtMS                 int64
}
type RegisteredDAT struct {
	ID, ParseStatus string
	Stats           CatalogStats
}
type ActivationState struct {
	Target      RuntimeTarget
	ParseStatus string
	Active      bool
}
type DATSelection struct {
	ID      string
	Target  RuntimeTarget
	AtMS    int64
	AuditID string
	Actor   authn.Actor
}
type CatalogPublication struct {
	DATID   string
	Catalog arcadedat.Catalog
	Replace bool
	AtMS    int64
}
type BIOSRequirement struct {
	ID, CoreID, ProviderID, TargetID, LogicalName                      string
	Mode, ConditionCode, Digest, MD5, SourceURL, VersionName, Delivery string
	Options, SHA256, EmulatorPath, ArchiveMembers                      *string
	SizeBytes                                                          *int64
	AtMS                                                               int64
}
type (
	Job         struct{ ID, State string }
	JobCreation struct {
		ID, DATID, DedupeKey, InputDigest string
		Input, Payload, Event             []byte
		AtMS                              int64
	}
)

type JobClaim struct {
	JobID, DATID                   string
	AtMS, DeadlineMS, LeaseUntilMS int64
	Event                          []byte
}
type JobFinish struct {
	JobID, DATID, State, Code string
	AtMS                      int64
	Event                     []byte
}
