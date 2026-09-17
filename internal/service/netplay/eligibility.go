package netplay

import (
	"context"
	model "retrom/internal/model/netplay"
	"strings"

	"retrom/internal/model/tagging"
)

func (service *Eligibility) Games(ctx context.Context, profileID, availability string) ([]model.GameSummary, error) {
	items := make([]model.GameSummary, 0)
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
) ([]model.GameSummary, bool, error) {
	if availability == "" {
		availability = "SUPPORTED"
	}
	if (availability != "SUPPORTED" && availability != "ALL") || limit < 1 || limit > 100 ||
		(afterGameID == "") != (afterTitle == "") {
		return nil, false, model.ErrInvalidProfile
	}
	items := make([]model.GameSummary, 0, limit+1)
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
	candidates []model.GameSummary,
	availability string,
	items []model.GameSummary,
	limit int,
) ([]model.GameSummary, string, string, error) {
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

func (service *Eligibility) attachGameTags(ctx context.Context, items []model.GameSummary) error {
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
	item model.GameSummary,
	availability string,
) (model.GameSummary, bool, error) {
	profiles, blocker, err := service.profileEligibility(ctx, item.GameID)
	if err != nil {
		return model.GameSummary{}, false, err
	}
	item.NetplayProfiles = make([]model.ProfileSummary, 0, len(profiles))
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

func (service *Eligibility) Profiles(ctx context.Context, gameID string) ([]model.EligibleProfile, error) {
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
