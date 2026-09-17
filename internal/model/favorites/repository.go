package favorites

import "context"

// Repository supplies a consistent read projection and named atomic commands.
type Repository interface { //nolint:interfacebloat // domain groups related named commands
	CommitFavorite(context.Context, FavoriteCommand) (State, error)
	CommitReplaceFolders(context.Context, ReplaceFoldersCommand) (State, error)
	CommitOrganize(context.Context, OrganizeCommand) (IdempotentResponse, error)
	CommitUnfavorite(context.Context, UnfavoriteCommand) (IdempotentResponse, error)
	CommitRestore(context.Context, RestoreCommand) (IdempotentResponse, error)
	CommitCreateFolder(context.Context, CreateFolderCommand) (IdempotentResponse, error)
	CommitRenameFolder(context.Context, RenameFolderCommand) (IdempotentResponse, error)
	CommitDeleteFolder(context.Context, DeleteFolderCommand) (IdempotentResponse, error)
	List(context.Context, string, ListOptions) (ListResult, error)
	Reference(context.Context, string, string) (*FavoriteReference, error)
	References(context.Context, string, []string) (map[string]FavoriteReference, error)
}

// FavoriteCommand ensures a game is favorited for a profile.
type FavoriteCommand struct {
	ProfileID, GameID string
	NowMS             int64
}

// ReplaceFoldersCommand replaces folder memberships for a favorited game.
type ReplaceFoldersCommand struct {
	ProfileID, GameID string
	FolderIDs         []string
	NowMS             int64
}

// IdempotencyEnvelope carries the common fields for idempotent commands.
type IdempotencyEnvelope struct {
	PrincipalID, Operation, Key, Digest string
	NowMS, ExpiresAtMS                  int64
}

// OrganizeCommand adds/removes folder memberships for multiple games atomically.
type OrganizeCommand struct {
	Idempotency     IdempotencyEnvelope
	ProfileID       string
	GameIDs         []string
	AddFolderIDs    []string
	RemoveFolderIDs []string
}

// UnfavoriteCommand removes favorites for the given games.
type UnfavoriteCommand struct {
	Idempotency IdempotencyEnvelope
	ProfileID   string
	GameIDs     []string
}

// RestoreCommand restores previously unfavorited games with their folder memberships.
type RestoreCommand struct {
	Idempotency IdempotencyEnvelope
	ProfileID   string
	Items       []RestoreItem
}

// CreateFolderCommand creates a new folder and optionally adds initial games.
type CreateFolderCommand struct {
	Idempotency IdempotencyEnvelope
	ProfileID   string
	FolderID    string
	Name        string
	NameKey     string
	GameIDs     []string
}

// RenameFolderCommand renames an existing folder.
type RenameFolderCommand struct {
	Idempotency     IdempotencyEnvelope
	ProfileID       string
	FolderID        string
	Name            string
	NameKey         string
	ExpectedVersion int64
}

// DeleteFolderCommand deletes a folder by ID and expected version.
type DeleteFolderCommand struct {
	Idempotency     IdempotencyEnvelope
	ProfileID       string
	FolderID        string
	ExpectedVersion int64
}

// FolderWrite carries the fields for a folder create or rename operation.
type FolderWrite struct {
	ProfileID, FolderID, Name, NameKey string
	ExpectedVersion, NowMS             int64
}
