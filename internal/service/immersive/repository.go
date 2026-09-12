package immersive

import "context"

type Repository interface {
	WithRead(context.Context, func(ReadScope) error) error
}
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
