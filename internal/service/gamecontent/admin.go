package gamecontent

import (
	"context"
	"errors"
	"fmt"

	model "retrom/internal/model/gamecontent"
)

// AdminGame loads the complete administrative game detail projection through
// one repository read snapshot.
func (service *Service) AdminGame(ctx context.Context, gameID string) (model.AdminGameDetail, error) {
	if gameID == "" || service.repository == nil {
		return model.AdminGameDetail{}, model.ErrInvalid
	}
	var result model.AdminGameDetail
	err := service.repository.WithRead(ctx, func(scope model.ReadScope) error {
		if scope.Admin == nil {
			return model.ErrInvalid
		}
		var err error
		result, err = scope.Admin.AdminGame(ctx, gameID)
		if err != nil {
			return fmt.Errorf("read admin game detail: %w", err)
		}
		return nil
	})
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
	var result model.AdminGamePatchResult
	err := service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		if scope.AdminWriter == nil {
			return model.ErrInvalid
		}
		state, err := scope.AdminWriter.LoadPatchState(ctx, request.GameID)
		if errors.Is(err, model.ErrAdminGameNotFound) {
			return model.ErrAdminGameNotFound
		}
		if err != nil {
			return fmt.Errorf("load admin game patch state: %w", err)
		}
		if state.Version != request.ExpectedVersion || state.Status != "PUBLISHED" {
			return model.ErrAdminGameVersionConflict
		}
		applyAdminGamePatch(&state.Metadata, request)
		changed, err := scope.AdminWriter.UpdatePatch(ctx, model.AdminGamePatchUpdate{
			GameID: request.GameID, ExpectedVersion: request.ExpectedVersion,
			Metadata: state.Metadata, Actor: request.Actor, NowMS: now,
		})
		if err != nil {
			return fmt.Errorf("persist admin game patch: %w", err)
		}
		if !changed {
			return model.ErrAdminGameVersionConflict
		}
		result = model.AdminGamePatchResult{Version: request.ExpectedVersion + 1, UpdatedAtMS: now}
		return nil
	})
	if err != nil {
		return model.AdminGamePatchResult{}, fmt.Errorf("patch admin game: %w", err)
	}
	return result, nil
}

func hasAdminGamePatch(request model.AdminGamePatchRequest) bool {
	return request.Title != nil || request.Description != nil || request.Developer != nil ||
		request.Publisher != nil || request.Genre != nil || request.PlayersPresent || request.ReleaseYearPresent
}

func applyAdminGamePatch(metadata *model.AdminGameMetadata, request model.AdminGamePatchRequest) {
	if request.Title != nil {
		metadata.Title = *request.Title
	}
	if request.Description != nil {
		metadata.Description = *request.Description
	}
	if request.Developer != nil {
		metadata.Developer = *request.Developer
	}
	if request.Publisher != nil {
		metadata.Publisher = *request.Publisher
	}
	if request.Genre != nil {
		metadata.Genre = *request.Genre
	}
	if request.PlayersPresent {
		metadata.Players = request.Players
	}
	if request.ReleaseYearPresent {
		metadata.ReleaseYear = request.ReleaseYear
	}
}
