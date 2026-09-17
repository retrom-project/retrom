package gamecontent

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/gamecontent"
)

type adminTestRepository struct {
	detail       model.AdminGameDetail
	readErr      error
	patchState   model.AdminGamePatchState
	patchErr     error
	updateErr    error
	updated      model.AdminGamePatchUpdate
	updateCalled bool
}

func (repository *adminTestRepository) WithRead(_ context.Context, work func(model.ReadScope) error) error {
	return work(model.ReadScope{Admin: repository})
}

func (repository *adminTestRepository) CommitWrite(_ context.Context, work func(model.WriteScope) error) error {
	return work(model.WriteScope{AdminWriter: repository})
}

func (repository *adminTestRepository) AdminGame(context.Context, string) (model.AdminGameDetail, error) {
	return repository.detail, repository.readErr
}

func (repository *adminTestRepository) LoadPatchState(context.Context, string) (model.AdminGamePatchState, error) {
	return repository.patchState, repository.patchErr
}

func (repository *adminTestRepository) UpdatePatch(_ context.Context, update model.AdminGamePatchUpdate) (bool, error) {
	repository.updated = update
	repository.updateCalled = true
	return repository.updateErr == nil, repository.updateErr
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
	oldTitle := "Old title"
	newTitle := "New title"
	repository := &adminTestRepository{patchState: model.AdminGamePatchState{
		Status: "PUBLISHED", Version: 4,
		Metadata: model.AdminGameMetadata{Title: oldTitle, Developer: "Dev"},
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
