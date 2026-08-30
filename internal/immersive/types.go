package immersive

import "errors"

var (
	ErrPlatformNotFound       = errors.New("IMMERSIVE_PLATFORM_NOT_FOUND")
	ErrLibraryNotFound        = errors.New("IMMERSIVE_LIBRARY_NOT_FOUND")
	ErrFavoriteFolderNotFound = errors.New("IMMERSIVE_FAVORITE_FOLDER_NOT_FOUND")
)

const (
	GameSortCode       = "IMMERSIVE_GAME_TITLE_INITIAL_ASC_V1"
	RecentGameSortCode = "IMMERSIVE_GAME_RECENT_DESC_V1"
	PageLimit          = 50
	LibraryAll         = "all"
	LibraryRecent      = "recent"
	LibraryFavorites   = "favorites"
	LibrarySaves       = "saves"
)

func ValidLibraryKind(value string) bool {
	switch value {
	case LibraryAll, LibraryRecent, LibraryFavorites, LibrarySaves:
		return true
	default:
		return false
	}
}

type Platform struct {
	ID             string
	Name           string
	GameCount      int64
	LastPlayedAtMS *int64
	FeaturedGames  []FeaturedGame
}

type FeaturedGame struct {
	PlatformID     string
	ID             string
	Title          string
	CoverAssetID   *string
	LastPlayedAtMS *int64
}

type NamedResource struct {
	ID   string
	Name string
}

type Game struct {
	ID               string
	Title            string
	TitleInitial     string
	Description      string
	ReleaseYear      *int64
	Developer        string
	Genre            string
	PlatformInstance NamedResource
	DefaultCore      NamedResource
	CoverAssetID     *string
	VideoAssetID     *string
	LastPlayedAtMS   *int64
	Favorited        bool
	SaveStates       []SaveState
}

type GameCursor struct {
	Title          string
	TitleInitial   string
	ID             string
	LastPlayedAtMS *int64
}

type GamePage struct {
	Platform   Platform
	Items      []Game
	NextCursor *GameCursor
}

type Destination struct {
	ID             string
	Kind           string
	Name           string
	GameCount      int64
	LastPlayedAtMS *int64
	FeaturedGames  []FeaturedGame
}

type FavoriteFolder struct {
	ID        string
	Name      string
	GameCount int64
}

type SaveState struct {
	ID            string
	Name          string
	CreatedAtMS   int64
	SizeBytes     int64
	DiscIndex     *int64
	HasScreenshot bool
}

type LibraryPage struct {
	Library    Destination
	Folder     *FavoriteFolder
	Folders    []FavoriteFolder
	Items      []Game
	NextCursor *GameCursor
}
