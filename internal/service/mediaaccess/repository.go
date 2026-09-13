package mediaaccess

import "context"

type Repository interface {
	WithRead(context.Context, func(Reader) error) error
}

type Reader interface {
	Game(context.Context, string) (GameAsset, bool, error)
	Save(context.Context, string) (SaveScreenshot, bool, error)
	Review(context.Context, string) ([]ReviewAsset, error)
	Sources(context.Context, string, string) ([]ReviewAsset, error)
}

type (
	Resource  struct{ Digest, MediaType string }
	GameAsset struct {
		Resource
		GameState string
	}
)

type SaveScreenshot struct {
	Resource
	ProfileID, GameState string
	Deleted              bool
}
type ReviewAsset struct {
	Resource
	Kind, State, ItemState, GameState string
	TerminalReview                    bool
}
