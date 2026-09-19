package netplay

import (
	"context"
	"errors"

	"retrom/internal/model/netplayprofile"

	"retrom/internal/model/corevalidation"
	"retrom/internal/model/tagging"
)

var ErrInvalidProfile = errors.New("NETPLAY_INVALID_PROFILE")

type EligibilityRepository interface {
	GamePage(context.Context, string, string, string, int) ([]GameSummary, bool, error)
	Rows(context.Context, string) ([]EligibilityRow, error)
	ArcadeDependencies(context.Context, string, string) ([]ArcadeDependencyRow, error)
	DependencyFileCount(context.Context, string, string, string) (int, error)
}

type TagReader interface {
	References(context.Context, []string) (map[string][]tagging.Reference, error)
}

type BIOSFactsReader interface {
	BIOS(context.Context, string, string) ([]corevalidation.BIOSRecord, error)
}

type EligibilityRow struct {
	VariantID, ProviderID, TargetID, BundleSHA256, SourceManifestDigest    string
	PlatformID, CoreID, CoreName, DependencyJSON, ContentKind, LogicalName string
	DATVersionID                                                           *string
}

type ArcadeDependencyRow struct {
	Kind, LogicalArchive, Machine, RequiredEntriesJSON, State string
}

type ProfileSummary struct {
	ID         string `json:"id"`
	CoreID     string `json:"coreId"`
	CoreName   string `json:"coreName"`
	ProviderID string `json:"providerId"`
	TargetID   string `json:"targetId"`
	MaxPlayers int    `json:"maxPlayers"`
}

type GameSummary struct {
	GameID               string              `json:"gameId"`
	Title                string              `json:"title"`
	CoverURL             *string             `json:"coverUrl"`
	PlatformID           string              `json:"platformId"`
	PlatformName         string              `json:"platformName"`
	PlatformInstanceID   string              `json:"platformInstanceId"`
	PlatformInstanceName string              `json:"platformInstanceName"`
	LastPlayedAtMS       *int64              `json:"lastPlayedAtMs"`
	AddedAtMS            int64               `json:"addedAtMs"`
	Availability         string              `json:"availability"`
	NetplayProfiles      []ProfileSummary    `json:"netplayProfiles"`
	BlockerCode          *string             `json:"blockerCode"`
	Tags                 []tagging.Reference `json:"tags"`
}

type EligibleProfile struct {
	Summary                ProfileSummary
	Manifest               netplayprofile.ManifestProfile
	VariantID              string
	BundleSHA256           string
	SourceManifestDigest   string
	DependencySnapshotJSON string
}
