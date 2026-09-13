package netplay

import (
	"context"

	repository "retrom/internal/repo/netplay"
	application "retrom/internal/service/netplay"
)

func eligibilityBlocker(hasVariant, content, core bool) string {
	return application.EligibilityBlocker(hasVariant, content, core)
}

func (service *Service) matchesTargetProfile(row eligibilityRow, candidate ManifestProfile) (bool, bool) {
	return service.eligibility().MatchesTargetProfile(row, candidate)
}

func (service *Service) arcadeDependencySnapshotRunnable(ctx context.Context, row eligibilityRow) (bool, error) {
	return service.eligibility().ArcadeSnapshotRunnable(ctx, row)
}

func (service *Service) queryEligibilityRows(ctx context.Context, gameID string) ([]eligibilityRow, error) {
	return repository.NewEligibility(service.database).Rows(ctx, gameID)
}
