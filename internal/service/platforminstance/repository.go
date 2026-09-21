package platforminstance

import (
	"context"

	"retrom/internal/platformcatalog"
)

// Repository supplies consistent read snapshots and atomic write capabilities.
type Repository interface {
	WithRead(context.Context, func(Reader) error) error
	WithWrite(context.Context, func(WriteScope) error) error
}

//nolint:interfacebloat // platform lifecycle and impact commands share one consistent read snapshot
type Reader interface {
	CatalogReferences(context.Context, platformcatalog.Catalog) (map[string]CatalogReference, error)
	Directories(context.Context) ([]Directory, error)
	Instance(context.Context, string) (Instance, error)
	CoreImpact(context.Context, string, string, int64) (CoreImpactFacts, error)
	UsedSlugs(context.Context, string, string) ([]string, error)
	CoreEnabled(context.Context, string, string) (bool, error)
}

type WriteScope struct {
	Reader      Reader
	Directories DirectoryWrites
	Idempotency IdempotencyRecords
}

//nolint:interfacebloat // directory mutations and their audit records share one atomic write scope
type DirectoryWrites interface {
	Insert(context.Context, NewDirectory) error
	RecordCreation(context.Context, CreationAudit) error
	Update(context.Context, DirectoryUpdate) (bool, error)
	Delete(context.Context, DirectoryDelete) (bool, error)
	ChangeDefaultCore(context.Context, DefaultCoreChange) (bool, error)
	RecordAudit(context.Context, AuditEvent) error
}

type Directory struct {
	ID, PlatformID, CoreID, Name, Description string
	SortOrder                                 int64
	Version                                   int64
	Enabled                                   bool
	CatalogKey                                *string
	Deleted                                   bool
}

type CatalogReference struct{ PlatformName, CoreName string }

type CoreImpactFacts struct {
	PlatformInstanceVersion int64
	PlatformID              string
	ProviderID              string
	TargetID                string
	BundleSHA256            string
	DATVersionID            *string
	Games                   []CoreImpactGame
}

type CoreImpactGame struct {
	GameID                  string  `json:"gameId"`
	GameVersion             int64   `json:"gameVersion"`
	VariantID               *string `json:"variantId"`
	VariantStatus           *string `json:"variantStatus"`
	TargetCompatibilityCode *string `json:"targetCompatibilityCode"`
}

type CoreImpact struct {
	Action                  string           `json:"action"`
	PlatformInstanceID      string           `json:"platformInstanceId"`
	PlatformInstanceVersion int64            `json:"platformInstanceVersion"`
	CoreID                  string           `json:"coreId"`
	ProviderID              string           `json:"providerId"`
	TargetID                string           `json:"targetId"`
	BundleSHA256            string           `json:"bundleSha256"`
	DATVersionID            *string          `json:"datVersionId"`
	Games                   []CoreImpactGame `json:"games"`
}

type CoreImpactItem struct {
	GameID      string  `json:"gameId"`
	Status      string  `json:"status"`
	BlockerCode *string `json:"blockerCode"`
}

type CoreImpactResult struct {
	Impact CoreImpact
	Counts map[string]int64
	Items  []CoreImpactItem
}

type DefaultCoreChangeResult struct {
	Version     int64
	UpdatedAtMS int64
}

type DirectoryUpdate struct {
	ID, Name, Description string
	SortOrder             int64
	Enabled               bool
	ExpectedVersion       int64
	UpdatedAtMS           int64
}

type DirectoryDelete struct {
	ID              string
	ExpectedVersion int64
	UpdatedAtMS     int64
}

type DefaultCoreChange struct {
	ID, CoreID      string
	ExpectedVersion int64
	UpdatedAtMS     int64
}

type AuditEvent struct {
	ID, Action, ResourceType, ResourceID string
	Actor                                AuditActor
	Before, After                        any
	CreatedAtMS                          int64
}

type NewDirectory struct {
	ID, Slug, CatalogKey string
	Input                CreateInput
	CreatedAtMS          int64
}

type CreationAudit struct {
	ID, Action string
	Actor      AuditActor
	Directory  NewDirectory
}

type IdempotencyKey struct{ PrincipalID, Operation, Key string }

type IdempotencyRecord struct {
	Digest                   string
	Response                 IdempotentResponse
	CreatedAtMS, ExpiresAtMS int64
}

type IdempotencyRecords interface {
	Find(context.Context, IdempotencyKey, int64) (IdempotencyRecord, bool, error)
	Save(context.Context, IdempotencyKey, IdempotencyRecord) error
}
