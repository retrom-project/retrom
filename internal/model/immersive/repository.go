package immersive

import "context"

// Repository provides consistent read snapshots for immersive views.
type Repository interface {
	LoadPlatforms(ctx context.Context, profileID string) ([]Platform, error)
	LoadGamePage(ctx context.Context, profileID, platformID string, limit int, cursor *GameCursor) (GamePage, error)
	LoadDestinations(ctx context.Context, profileID string) ([]Destination, error)
	LoadLibraryPage(ctx context.Context, profileID, kind, folderID string, limit int, cursor *GameCursor) (LibraryPage, error)
}

// ReadScope contains the projections needed internally by the repository.
type ReadScope struct {
	Platforms PlatformReader
	Libraries LibraryReader
	Saves     SaveReader
}

type PlatformReader interface {
	Platforms(context.Context, string) ([]Platform, error)
	Platform(context.Context, string, string) (Platform, error)
	Featured(context.Context, string, string) ([]FeaturedGame, error)
	Games(context.Context, string, string, int, *GameCursor) ([]Game, error)
}

type LibraryReader interface {
	Summary(context.Context, string, string, string) (Destination, error)
	Featured(context.Context, string, string, string) ([]FeaturedGame, error)
	Folder(context.Context, string, string) (*FavoriteFolder, error)
	Folders(context.Context, string) ([]FavoriteFolder, error)
	Games(context.Context, string, string, string, int, *GameCursor) ([]Game, error)
}

type SaveReader interface {
	ForGames(context.Context, string, []string) (map[string][]SaveState, error)
}
