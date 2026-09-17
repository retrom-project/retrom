package platforminstance

import (
	"context"

	"retrom/internal/capability/runtime/platformcatalog"
)

// Repository supplies consistent read snapshots and named atomic commands.
type Repository interface { //nolint:interfacebloat // domain groups related lifecycle commands
	LoadCatalogReferences(context.Context, platformcatalog.Catalog) (map[string]CatalogReference, error)
	LoadDirectories(context.Context) ([]Directory, error)
	LoadInstance(context.Context, string) (Instance, error)
	LoadCoreImpactFacts(context.Context, string, string, int64) (CoreImpactFacts, error)
	CommitCreate(context.Context, CreateCommand) (Instance, error)
	CommitPatch(context.Context, PatchCommand) (PatchResult, error)
	CommitReorder(context.Context, ReorderCommand) ([]ReorderResult, error)
	CommitDelete(context.Context, DeleteCommand) error
	CommitChangeDefaultCore(context.Context, ChangeDefaultCoreCommand) (DefaultCoreChangeResult, error)
	CommitApply(context.Context, ApplyCommand) (IdempotentResponse, error)
}

// CreateCommand carries all values for creating a new platform instance.
type CreateCommand struct {
	Actor      AuditActor
	Input      CreateInput
	CatalogKey string
	Action     string
	NowMS      int64
	ID         string
	AuditID    string
}

// PatchCommand carries values for updating a platform instance.
type PatchCommand struct {
	ID              string
	ExpectedVersion int64
	Name            *string
	Description     *string
	SortOrder       *int64
	Enabled         *bool
	Actor           AuditActor
	NowMS           int64
	AuditID         string
}

// PatchResult holds the result of a patch operation.
type PatchResult struct {
	ID          string
	Name        string
	Description string
	SortOrder   int64
	Enabled     bool
	Version     int64
	UpdatedAtMS int64
}

// ReorderCommand carries values for reordering platform instances.
type ReorderCommand struct {
	Actor AuditActor
	Items []ReorderItem
	NowMS int64
}

// ReorderItem carries a single item for reordering.
type ReorderItem struct {
	ID      string
	Version int64
}

// ReorderResult holds the result of a single reorder update.
type ReorderResult struct {
	ID          string
	SortOrder   int64
	Version     int64
	UpdatedAtMS int64
}

// DeleteCommand carries values for soft-deleting a platform instance.
type DeleteCommand struct {
	ID              string
	ExpectedVersion int64
	Actor           AuditActor
	NowMS           int64
	AuditID         string
}

// ChangeDefaultCoreCommand carries values for changing the default core.
type ChangeDefaultCoreCommand struct {
	InstanceID     string
	CoreID         string
	Expected       int64
	Digest         string
	ConfirmBlocked bool
	Actor          AuditActor
	NowMS          int64
	AuditID        string
}

// ApplyCommand carries values for applying catalog recommendations.
type ApplyCommand struct {
	IdempotencyKey IdempotencyKey
	Digest         string
	Actor          AuditActor
	Catalog        platformcatalog.Catalog
	NowMS          int64
	ExpiresAtMS    int64
	InstanceIDs    []string
	AuditIDs       []string
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
