package immersive

import "errors"

var ErrPlatformNotFound = errors.New("IMMERSIVE_PLATFORM_NOT_FOUND")

const (
	GameSortCode = "IMMERSIVE_GAME_TITLE_ASC"
	PageLimit    = 50
)

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
	Description      string
	ReleaseYear      *int64
	Developer        string
	Genre            string
	PlatformInstance NamedResource
	DefaultCore      NamedResource
	CoverAssetID     *string
	VideoAssetID     *string
	LastPlayedAtMS   *int64
}

type GameCursor struct {
	Title string
	ID    string
}

type GamePage struct {
	Platform   Platform
	Items      []Game
	NextCursor *GameCursor
}
