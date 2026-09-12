package netplay

import (
	"context"

	validationrepository "retrom/internal/persistence/corevalidation"
	repository "retrom/internal/persistence/netplay"
	validation "retrom/internal/service/corevalidation"
	application "retrom/internal/service/netplay"
)

type (
	GameSummary     = application.GameSummary
	ProfileSummary  = application.ProfileSummary
	eligibleProfile = application.EligibleProfile
	eligibilityRow  = application.EligibilityRow
)

func (service *Service) eligibility() *application.Eligibility {
	return application.NewEligibility(
		repository.NewEligibility(service.database), service.registry, service.tags,
		validation.New(validationrepository.New(service.database)),
	)
}

func (service *Service) Games(ctx context.Context, profileID, availability string) ([]GameSummary, error) {
	items, err := service.eligibility().Games(ctx, profileID, availability)
	if err != nil {
		return nil, serviceError("games", err)
	}
	return items, nil
}

func (service *Service) GamePage(
	ctx context.Context, profileID, availability, title, gameID string, limit int,
) ([]GameSummary, bool, error) {
	items, more, err := service.eligibility().GamePage(ctx, profileID, availability, title, gameID, limit)
	if err != nil {
		return nil, false, serviceError("game page", err)
	}
	return items, more, nil
}

func (service *Service) eligibleProfiles(ctx context.Context, gameID string) ([]eligibleProfile, error) {
	items, err := service.eligibility().Profiles(ctx, gameID)
	if err != nil {
		return nil, serviceError("eligible profiles", err)
	}
	return items, nil
}
