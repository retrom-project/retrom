package gamecontent

import (
	"context"
	"fmt"

	model "retrom/internal/model/gamecontent"
)

// DeleteAdminGame validates and persists the administrative game tombstone in
// one repository transaction. The repository port includes every side effect
// that must be atomic with the game state transition.
func (service *Service) DeleteAdminGame(
	ctx context.Context, request model.DeleteGameRequest,
) (model.DeleteGameResult, error) {
	if service.repository == nil || service.now == nil {
		return model.DeleteGameResult{}, model.ErrInvalid
	}
	if err := validateDeleteGameRequest(request); err != nil {
		return model.DeleteGameResult{}, err
	}
	now := request.NowMS
	if now <= 0 {
		now = service.now().UnixMilli()
	}
	request.NowMS = now
	result, err := service.repository.CommitDeleteGame(ctx, request)
	if err != nil {
		return model.DeleteGameResult{}, fmt.Errorf("delete admin game: %w", err)
	}
	if result.PayloadReleaseQueued && service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
	return result, nil
}

func validateDeleteGameRequest(request model.DeleteGameRequest) error {
	if request.GameID == "" || request.PrincipalID == "" || request.Key == "" ||
		request.RequestDigest == "" || request.ExpectedVersion < 1 || request.ImpactDigest == "" {
		return model.ErrInvalid
	}
	return nil
}
