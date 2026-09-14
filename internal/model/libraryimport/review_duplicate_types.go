package libraryimport

import (
	"context"
	"errors"
)

var ErrMultiDiscIncomplete = errors.New("MULTI_DISC_IDENTITY_INCOMPLETE")

type DuplicateGame struct {
	GameID               string `json:"gameId"`
	Title                string `json:"title"`
	PlatformInstanceID   string `json:"platformInstanceId"`
	PlatformInstanceName string `json:"platformInstanceName"`
}
type (
	ContentSnapshot     struct{ ID, Kind string }
	ContentIdentityPart struct {
		Role, SHA256 string
		Count        int64
	}
)

type (
	ContentIdentityDisc    struct{ State, SHA256 string }
	DuplicateQuery         struct{ SnapshotID, PlatformID, ContentKind string }
	ContentDuplicateReader interface {
		Snapshot(context.Context, string) (ContentSnapshot, error)
		IdentityParts(context.Context, string) ([]ContentIdentityPart, error)
		OrderedDiscs(context.Context, string) ([]ContentIdentityDisc, error)
		PublishedMatches(context.Context, DuplicateQuery) ([]DuplicateGame, error)
		ReviewPlatform(context.Context, string) (string, error)
	}
)
