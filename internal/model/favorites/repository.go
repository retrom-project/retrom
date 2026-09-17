package favorites

import "context"

// Repository supplies a consistent read projection and transaction-scoped writes.
type Repository interface {
	CommitWrite(context.Context, func(WriteScope) error) error
	List(context.Context, string, ListOptions) (ListResult, error)
	Reference(context.Context, string, string) (*FavoriteReference, error)
	References(context.Context, string, []string) (map[string]FavoriteReference, error)
}

// WriteScope binds every capability to the same atomic operation.
type WriteScope struct {
	Games        FavoriteRecords
	Memberships  MembershipRecords
	Folders      FolderRecords
	FolderWrites FolderWrites
	Idempotency  IdempotencyRecords
}

type FavoriteRecords interface {
	Visible(context.Context, string) (bool, error)
	RequireVisible(context.Context, []string) error
	State(context.Context, string, string) (State, bool, error)
	Ensure(context.Context, string, string, int64) error
	Remove(context.Context, string, string) error
}

type MembershipRecords interface {
	FolderIDs(context.Context, string, string) ([]string, error)
	Add(context.Context, string, string, string, int64) error
	Remove(context.Context, string, string, string) error
}

type FolderRecords interface {
	Require(context.Context, string, []string) error
	Existing(context.Context, string, []string) (map[string]struct{}, error)
	RequireAvailableName(context.Context, string, string, string) error
	Get(context.Context, string, string) (Folder, error)
	Count(context.Context, string) (int, error)
}

type FolderWrites interface {
	Create(context.Context, FolderWrite) error
	Rename(context.Context, FolderWrite) error
	Delete(context.Context, string, string, int64) error
}

type FolderWrite struct {
	ProfileID, FolderID, Name, NameKey string
	ExpectedVersion, NowMS             int64
}

type IdempotencyKey struct {
	PrincipalID, Operation, Key string
}

type IdempotencyRecord struct {
	Digest                   string
	Response                 IdempotentResponse
	CreatedAtMS, ExpiresAtMS int64
}

type IdempotencyRecords interface {
	Find(context.Context, IdempotencyKey, int64) (IdempotencyRecord, bool, error)
	Save(context.Context, IdempotencyKey, IdempotencyRecord) error
}
