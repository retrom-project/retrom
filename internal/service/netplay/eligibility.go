package netplay

import (
	"context"
	"strings"

	"retrom/internal/service/tagging"
	"retrom/internal/transport/netplay/profile"
)

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
	Manifest               profile.ManifestProfile
	VariantID              string
	BundleSHA256           string
	SourceManifestDigest   string
	DependencySnapshotJSON string
}

func (service *Eligibility) Games(ctx context.Context, profileID, availability string) ([]GameSummary, error) {
	items := make([]GameSummary, 0)
	afterTitle, afterGameID := "", ""
	for {
		page, hasMore, err := service.GamePage(ctx, profileID, availability, afterTitle, afterGameID, 100)
		if err != nil {
			return nil, err
		}
		items = append(items, page...)
		if !hasMore {
			return items, nil
		}
		last := page[len(page)-1]
		afterTitle, afterGameID = strings.ToLower(last.Title), last.GameID
	}
}

func (service *Eligibility) GamePage(
	ctx context.Context,
	profileID, availability, afterTitle, afterGameID string,
	limit int,
) ([]GameSummary, bool, error) {
	if availability == "" {
		availability = "SUPPORTED"
	}
	if (availability != "SUPPORTED" && availability != "ALL") || limit < 1 || limit > 100 ||
		(afterGameID == "") != (afterTitle == "") {
		return nil, false, ErrInvalidProfile
	}
	items := make([]GameSummary, 0, limit+1)
	scanTitle, scanGameID := afterTitle, afterGameID
	for len(items) <= limit {
		candidates, hasMoreCandidates, err := service.repository.GamePage(
			ctx, profileID, scanTitle, scanGameID, limit+1,
		)
		if err != nil {
			return nil, false, serviceError("game page", err)
		}
		items, scanTitle, scanGameID, err = service.appendEligibleGames(
			ctx, candidates, availability, items, limit,
		)
		if err != nil {
			return nil, false, err
		}
		if len(items) > limit {
			break
		}
		if !hasMoreCandidates {
			if err := service.attachGameTags(ctx, items); err != nil {
				return nil, false, err
			}
			return items, false, nil
		}
	}
	items = items[:limit]
	if err := service.attachGameTags(ctx, items); err != nil {
		return nil, false, err
	}
	return items, true, nil
}

func (service *Eligibility) appendEligibleGames(
	ctx context.Context,
	candidates []GameSummary,
	availability string,
	items []GameSummary,
	limit int,
) ([]GameSummary, string, string, error) {
	lastTitle, lastGameID := "", ""
	for _, candidate := range candidates {
		lastTitle, lastGameID = strings.ToLower(candidate.Title), candidate.GameID
		item, include, err := service.enrichGame(ctx, candidate, availability)
		if err != nil {
			return nil, "", "", err
		}
		if include {
			items = append(items, item)
			if len(items) > limit {
				break
			}
		}
	}
	return items, lastTitle, lastGameID, nil
}

func (service *Eligibility) attachGameTags(ctx context.Context, items []GameSummary) error {
	gameIDs := make([]string, 0, len(items))
	for _, item := range items {
		gameIDs = append(gameIDs, item.GameID)
	}
	references, err := service.tags.References(ctx, gameIDs)
	if err != nil {
		return serviceError("list game tags", err)
	}
	for index := range items {
		items[index].Tags = references[items[index].GameID]
		if items[index].Tags == nil {
			items[index].Tags = []tagging.Reference{}
		}
	}
	return nil
}

func (service *Eligibility) enrichGame(
	ctx context.Context,
	item GameSummary,
	availability string,
) (GameSummary, bool, error) {
	profiles, blocker, err := service.profileEligibility(ctx, item.GameID)
	if err != nil {
		return GameSummary{}, false, err
	}
	item.NetplayProfiles = make([]ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		item.NetplayProfiles = append(item.NetplayProfiles, profile.Summary)
	}
	if len(item.NetplayProfiles) > 0 {
		item.Availability = "SUPPORTED"
	} else {
		item.Availability = "UNSUPPORTED"
		item.BlockerCode = &blocker
	}
	return item, availability == "ALL" || item.Availability == "SUPPORTED", nil
}

func (service *Eligibility) Profiles(ctx context.Context, gameID string) ([]EligibleProfile, error) {
	profiles, _, err := service.profileEligibility(ctx, gameID)
	return profiles, err
}

func EligibilityBlocker(hasVariant, contentKindAllowed, coreAllowed bool) string {
	if !hasVariant {
		return "GAME_UNAVAILABLE"
	}
	if !contentKindAllowed {
		return "CONTENT_NOT_ALLOWLISTED"
	}
	if !coreAllowed {
		return "CORE_NOT_ALLOWLISTED"
	}
	return "DEPENDENCY_STALE"
}
