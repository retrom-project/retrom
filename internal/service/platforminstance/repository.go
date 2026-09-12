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

type Reader interface {
	CatalogReferences(context.Context, platformcatalog.Catalog) (map[string]CatalogReference, error)
	Directories(context.Context) ([]Directory, error)
	Instance(context.Context, string) (Instance, error)
	UsedSlugs(context.Context, string, string) ([]string, error)
	CoreEnabled(context.Context, string, string) (bool, error)
}

type WriteScope struct {
	Reader      Reader
	Directories DirectoryWrites
	Idempotency IdempotencyRecords
}

type DirectoryWrites interface {
	Insert(context.Context, NewDirectory) error
	RecordCreation(context.Context, CreationAudit) error
}

type Directory struct {
	ID, PlatformID, CoreID, Name, Description string
	SortOrder                                 int64
	Enabled                                   bool
	CatalogKey                                *string
	Deleted                                   bool
}

type CatalogReference struct{ PlatformName, CoreName string }

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
