package gamecontent

import (
	"context"
	"fmt"

	model "retrom/internal/model/gamecontent"
)

// AdminGame loads the complete administrative game detail projection through
// one repository read snapshot.
func (service *Service) AdminGame(ctx context.Context, gameID string) (model.AdminGameDetail, error) {
	if gameID == "" || service.repository == nil {
		return model.AdminGameDetail{}, model.ErrInvalid
	}
	result, err := service.repository.ReadAdminGame(ctx, gameID)
	if err != nil {
		return model.AdminGameDetail{}, fmt.Errorf("read admin game: %w", err)
	}
	return result, nil
}

// PatchAdminGame applies an optimistic-locking metadata patch and records its
// audit event in the same persistence transaction.
func (service *Service) PatchAdminGame(
	ctx context.Context, request model.AdminGamePatchRequest,
) (model.AdminGamePatchResult, error) {
	if request.GameID == "" || request.ExpectedVersion < 1 || !hasAdminGamePatch(request) || service.repository == nil {
		return model.AdminGamePatchResult{}, model.ErrInvalid
	}
	now := request.NowMS
	if now <= 0 {
		now = service.now().UnixMilli()
	}
	request.NowMS = now
	result, err := service.repository.CommitPatchGame(ctx, request)
	if err != nil {
		return model.AdminGamePatchResult{}, fmt.Errorf("patch admin game: %w", err)
	}
	return result, nil
}

func hasAdminGamePatch(request model.AdminGamePatchRequest) bool {
	return request.Title != nil || request.Description != nil || request.Developer != nil ||
		request.Publisher != nil || request.Genre != nil || request.PlayersPresent || request.ReleaseYearPresent
}
