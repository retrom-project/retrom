package gamecontent

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/gamecontent"
)

type adminTestRepository struct {
	model.Repository
	detail       model.AdminGameDetail
	readErr      error
	patchState   model.AdminGamePatchState
	patchErr     error
	updateErr    error
	updated      model.AdminGamePatchUpdate
	updateCalled bool
}

func (repository *adminTestRepository) ReadAdminGame(_ context.Context, _ string) (model.AdminGameDetail, error) {
	return repository.detail, repository.readErr
}

func (repository *adminTestRepository) CommitPatchGame(_ context.Context, request model.AdminGamePatchRequest) (model.AdminGamePatchResult, error) {
	state, err := repository.loadAndPatch(request)
	if err != nil {
		return model.AdminGamePatchResult{}, err
	}
	return state, nil
}

func (repository *adminTestRepository) loadAndPatch(request model.AdminGamePatchRequest) (model.AdminGamePatchResult, error) {
	if repository.patchErr != nil {
		return model.AdminGamePatchResult{}, repository.patchErr
	}
	state := repository.patchState
	if state.Version != request.ExpectedVersion || state.Status != "PUBLISHED" {
		return model.AdminGamePatchResult{}, model.ErrAdminGameVersionConflict
	}
	applyAdminGamePatch(&state.Metadata, request)
	repository.updated = model.AdminGamePatchUpdate{
		GameID: request.GameID, ExpectedVersion: request.ExpectedVersion,
		Metadata: state.Metadata, Actor: request.Actor, NowMS: request.NowMS,
	}
	repository.updateCalled = true
	if repository.updateErr != nil {
		return model.AdminGamePatchResult{}, repository.updateErr
	}
	return model.AdminGamePatchResult{
		Version:     request.ExpectedVersion + 1,
		UpdatedAtMS: request.NowMS,
	}, nil
}

func applyAdminGamePatch(metadata *model.AdminGameMetadata, r model.AdminGamePatchRequest) {
	if r.Title != nil {
		metadata.Title = *r.Title
	}
	if r.Description != nil {
		metadata.Description = *r.Description
	}
	if r.Developer != nil {
		metadata.Developer = *r.Developer
	}
	if r.Publisher != nil {
		metadata.Publisher = *r.Publisher
	}
	if r.Genre != nil {
		metadata.Genre = *r.Genre
	}
	if r.PlayersPresent {
		metadata.Players = r.Players
	}
	if r.ReleaseYearPresent {
		metadata.ReleaseYear = r.ReleaseYear
	}
}

func TestAdminGameReadsCompleteProjectionThroughRepository(t *testing.T) {
	repository := &adminTestRepository{detail: model.AdminGameDetail{
		Title: "Fixture", Version: 3,
		Files:    []model.AdminGameFile{{Role: "CONTENT", LogicalName: "fixture.rom"}},
		Assets:   []model.AdminGameAsset{{ID: "asset"}},
		Variants: []model.AdminGameVariant{{ID: "variant"}},
	}}
	service := New(repository, func() time.Time { return time.UnixMilli(100) })

	detail, err := service.AdminGame(t.Context(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Title != "Fixture" || detail.Version != 3 || len(detail.Files) != 1 ||
		len(detail.Assets) != 1 || len(detail.Variants) != 1 {
		t.Fatalf("unexpected admin detail: %+v", detail)
	}
}

func TestPatchAdminGameAppliesFieldsAndKeepsAtomicPort(t *testing.T) {
	newTitle := "New title"
	repository := &adminTestRepository{patchState: model.AdminGamePatchState{
		Status: "PUBLISHED", Version: 4,
		Metadata: model.AdminGameMetadata{Title: "Old title", Developer: "Dev"},
	}}
	service := New(repository, func() time.Time { return time.UnixMilli(100) })

	result, err := service.PatchAdminGame(t.Context(), model.AdminGamePatchRequest{
		GameID: "game", ExpectedVersion: 4, Title: &newTitle,
		PlayersPresent: true, Players: nil, NowMS: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 5 || result.UpdatedAtMS != 200 || !repository.updateCalled {
		t.Fatalf("unexpected patch result: %+v, update called=%v", result, repository.updateCalled)
	}
	if repository.updated.Metadata.Title != newTitle || repository.updated.Metadata.Developer != "Dev" ||
		repository.updated.Metadata.Players != nil || repository.updated.ExpectedVersion != 4 {
		t.Fatalf("unexpected patch update: %+v", repository.updated)
	}
}

func TestPatchAdminGameRejectsStaleOrNonPublishedState(t *testing.T) {
	for _, test := range []struct {
		name, status string
		version      int64
	}{
		{name: "stale", status: "PUBLISHED", version: 3},
		{name: "deleted", status: "DELETED", version: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &adminTestRepository{patchState: model.AdminGamePatchState{
				Status: test.status, Version: test.version,
				Metadata: model.AdminGameMetadata{Title: "Fixture"},
			}}
			service := New(repository, time.Now)
			title := "Changed"
			_, err := service.PatchAdminGame(t.Context(), model.AdminGamePatchRequest{
				GameID: "game", ExpectedVersion: 4, Title: &title,
			})
			if !errors.Is(err, model.ErrAdminGameVersionConflict) {
				t.Fatalf("expected version conflict, got %v", err)
			}
			if repository.updateCalled {
				t.Fatal("conflicting patch reached update port")
			}
		})
	}
}
