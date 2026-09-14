package favorites

import model "retrom/internal/model/favorites"

type (
	BatchResult        = model.BatchResult
	FavoriteRecords    = model.FavoriteRecords
	FavoriteReference  = model.FavoriteReference
	Folder             = model.Folder
	FolderRecords      = model.FolderRecords
	FolderWrite        = model.FolderWrite
	FolderWrites       = model.FolderWrites
	GameItem           = model.GameItem
	IdempotencyKey     = model.IdempotencyKey
	IdempotencyRecord  = model.IdempotencyRecord
	IdempotencyRecords = model.IdempotencyRecords
	IdempotentResponse = model.IdempotentResponse
	ListOptions        = model.ListOptions
	ListResult         = model.ListResult
	MembershipRecords  = model.MembershipRecords
	NamedResource      = model.NamedResource
	PageCursor         = model.PageCursor
	PlatformSummary    = model.PlatformSummary
	Principal          = model.Principal
	Repository         = model.Repository
	RestoreItem        = model.RestoreItem
	RestoreResult      = model.RestoreResult
	State              = model.State
	Summary            = model.Summary
	UnfavoriteItem     = model.UnfavoriteItem
	UnfavoriteResult   = model.UnfavoriteResult
	WriteScope         = model.WriteScope
)

const (
	MaxFolders            = model.MaxFolders
	MaxOrganizeEdges      = model.MaxOrganizeEdges
	MaxOrganizeFolders    = model.MaxOrganizeFolders
	MaxOrganizeGames      = model.MaxOrganizeGames
	MaxRestoreFolderEdges = model.MaxRestoreFolderEdges
	MaxRestoreGames       = model.MaxRestoreGames
	MaxUnfavoriteGames    = model.MaxUnfavoriteGames
	ScopeAll              = model.ScopeAll
	ScopeFolder           = model.ScopeFolder
	ScopeUncategorized    = model.ScopeUncategorized
	SortFavoritedDesc     = model.SortFavoritedDesc
	SortRecentlyPlayed    = model.SortRecentlyPlayed
	SortReleaseYearDesc   = model.SortReleaseYearDesc
	SortTitleAsc          = model.SortTitleAsc
)

var (
	ErrBatchTooLarge           = model.ErrBatchTooLarge
	ErrFolderLimit             = model.ErrFolderLimit
	ErrFolderNameConflict      = model.ErrFolderNameConflict
	ErrFolderNotFound          = model.ErrFolderNotFound
	ErrGameNotFound            = model.ErrGameNotFound
	ErrIdempotencyReused       = model.ErrIdempotencyReused
	ErrInvalid                 = model.ErrInvalid
	ErrInvalidCursor           = model.ErrInvalidCursor
	ErrInvalidFavoriteListSort = model.ErrInvalidFavoriteListSort
	ErrInvalidFolderName       = model.ErrInvalidFolderName
	ErrInvariant               = model.ErrInvariant
	ErrVersionConflict         = model.ErrVersionConflict
)

var ValidID = model.ValidID
