package immersive

import model "retrom/internal/model/immersive"

var ValidLibraryKind = model.ValidLibraryKind

type (
	Destination    = model.Destination
	FavoriteFolder = model.FavoriteFolder
	FeaturedGame   = model.FeaturedGame
	Game           = model.Game
	GameCursor     = model.GameCursor
	GamePage       = model.GamePage
	LibraryPage    = model.LibraryPage
	LibraryReader  = model.LibraryReader
	NamedResource  = model.NamedResource
	Platform       = model.Platform
	PlatformReader = model.PlatformReader
	ReadScope      = model.ReadScope
	Repository     = model.Repository
	SaveReader     = model.SaveReader
	SaveState      = model.SaveState
)

const (
	GameSortCode       = model.GameSortCode
	LibraryAll         = model.LibraryAll
	LibraryFavorites   = model.LibraryFavorites
	LibraryRecent      = model.LibraryRecent
	LibrarySaves       = model.LibrarySaves
	PageLimit          = model.PageLimit
	RecentGameSortCode = model.RecentGameSortCode
)

var (
	ErrFavoriteFolderNotFound = model.ErrFavoriteFolderNotFound
	ErrLibraryNotFound        = model.ErrLibraryNotFound
	ErrPlatformNotFound       = model.ErrPlatformNotFound
)
