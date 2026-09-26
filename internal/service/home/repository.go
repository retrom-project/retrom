package home

import (
	"context"

	"retrom/internal/service/tagging"
)

//nolint:interfacebloat // the dashboard reads share one cohesive projection boundary
type Repository interface {
	Summary(context.Context, string) (Summary, error)
	RecentSaves(context.Context, string) ([]RecentSave, error)
	RecentGames(context.Context, string, bool) ([]RecentGame, error)
	LatestGames(context.Context) ([]LatestGame, error)
	FeaturedGame(context.Context, string) (FeaturedGame, bool, error)
	Platforms(context.Context, string) ([]Platform, error)
}

type TagReader interface {
	References(context.Context, []string) (map[string][]tagging.Reference, error)
}

type Tag = tagging.Reference

type Summary struct {
	GameCount, SaveCount, ReviewCount, ActiveDurationMS int64
}

type Resource struct{ ID, Name string }

type RecentSave struct {
	SaveStateID, GameID, GameTitle, Name string
	CreatedAtMS, ActiveDurationMS        int64
	LastSyncedAtMS, DiscIndex            *int64
	HasScreenshot                        bool
	Tags                                 []Tag
}

type RecentGame struct {
	GameID, Title, Status, Availability string
	Platform, PlatformInstance          Resource
	LastPlayedAtMS, ActiveDurationMS    int64
	SessionCount                        int64
	CoverAssetID                        *string
	Tags                                []Tag
}

type LatestGame struct {
	GameID, Title              string
	Platform, PlatformInstance Resource
	CreatedAtMS                int64
	CoverAssetID               *string
	Tags                       []Tag
}

type FeaturedSave struct {
	SaveStateID, ScreenshotID     string
	CreatedAtMS, ActiveDurationMS int64
	DiscIndex                     *int64
	HasScreenshot                 bool
}

type FeaturedGame struct {
	LaunchID, GameID, Title, Description string
	Platform, PlatformInstance           Resource
	LastPlayedAtMS, ActiveDurationMS     int64
	SessionCount, SaveCount              int64
	CoverAssetID                         *string
	DefaultDOSEntry                      *string
	LastSessionSave                      *FeaturedSave
	Tags                                 []Tag
}

type Platform struct {
	ID, Name             string
	GameCount, PlayCount int64
}

type Data struct {
	Summary        Summary
	FeaturedGame   *FeaturedGame
	LatestGames    []LatestGame
	RecentGames    []RecentGame
	RecentSaves    []RecentSave
	Platforms      []Platform
	QuickPlatforms []Platform
}
