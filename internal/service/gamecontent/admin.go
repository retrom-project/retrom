package gamecontent

import (
	"context"
	"errors"
	"fmt"
)

// AdminGame loads the complete administrative game detail projection through
// one repository read snapshot.
func (service *Service) AdminGame(ctx context.Context, gameID string) (AdminGameDetail, error) {
	if gameID == "" || service.repository == nil {
		return AdminGameDetail{}, ErrInvalid
	}
	var result AdminGameDetail
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		if scope.Admin == nil {
			return ErrInvalid
		}
		var err error
		result, err = scope.Admin.AdminGame(ctx, gameID)
		if err != nil {
			return fmt.Errorf("read admin game detail: %w", err)
		}
		return nil
	})
	if err != nil {
		return AdminGameDetail{}, fmt.Errorf("read admin game: %w", err)
	}
	return result, nil
}

// PatchAdminGame applies an optimistic-locking metadata patch and records its
// audit event in the same persistence transaction.
func (service *Service) PatchAdminGame(
	ctx context.Context, request AdminGamePatchRequest,
) (AdminGamePatchResult, error) {
	if request.GameID == "" || request.ExpectedVersion < 1 || !hasAdminGamePatch(request) || service.repository == nil {
		return AdminGamePatchResult{}, ErrInvalid
	}
	now := request.NowMS
	if now <= 0 {
		now = service.now().UnixMilli()
	}
	var result AdminGamePatchResult
	err := service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		if scope.AdminWriter == nil {
			return ErrInvalid
		}
		state, err := scope.AdminWriter.LoadPatchState(ctx, request.GameID)
		if errors.Is(err, ErrAdminGameNotFound) {
			return ErrAdminGameNotFound
		}
		if err != nil {
			return fmt.Errorf("load admin game patch state: %w", err)
		}
		if state.Version != request.ExpectedVersion || state.Status != "PUBLISHED" {
			return ErrAdminGameVersionConflict
		}
		applyAdminGamePatch(&state.Metadata, request)
		changed, err := scope.AdminWriter.UpdatePatch(ctx, AdminGamePatchUpdate{
			GameID: request.GameID, ExpectedVersion: request.ExpectedVersion,
			Metadata: state.Metadata, Actor: request.Actor, NowMS: now,
		})
		if err != nil {
			return fmt.Errorf("persist admin game patch: %w", err)
		}
		if !changed {
			return ErrAdminGameVersionConflict
		}
		result = AdminGamePatchResult{Version: request.ExpectedVersion + 1, UpdatedAtMS: now}
		return nil
	})
	if err != nil {
		return AdminGamePatchResult{}, fmt.Errorf("patch admin game: %w", err)
	}
	return result, nil
}

func hasAdminGamePatch(request AdminGamePatchRequest) bool {
	return request.Title != nil || request.Description != nil || request.Developer != nil ||
		request.Publisher != nil || request.Genre != nil || request.PlayersPresent || request.ReleaseYearPresent
}

func applyAdminGamePatch(metadata *AdminGameMetadata, request AdminGamePatchRequest) {
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
