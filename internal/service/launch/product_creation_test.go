package launch

import (
	"context"
	"errors"
	"testing"

	gamevariant "retrom/internal/service/gamevariant"
)

type productCreationMemory struct{ failure error }

func (repository *productCreationMemory) Replay(context.Context, ProductCreateCommand) (ProductReceipt, bool, error) {
	return ProductReceipt{}, false, nil
}

func (repository *productCreationMemory) Snapshot(context.Context, ProductCreateCommand) (ProductSnapshot, error) {
	return ProductSnapshot{}, repository.failure
}

func (repository *productCreationMemory) WithCreation(context.Context, func(ProductCreationScope) error) error {
	return repository.failure
}

func TestProductCreatorRetainsSnapshotFailure(t *testing.T) {
	cause := errors.New("product snapshot unavailable")
	creator := NewProductCreator(&productCreationMemory{failure: cause}, nil, nil, ProductEnvironment{})
	result, err := creator.Create(t.Context(), ProductCreateCommand{ProfileID: "profile", Request: CreateRequest{GameID: "game", ReturnTo: "/games/game"}})
	if !errors.Is(err, cause) || result.Created.LaunchID != "" {
		t.Fatalf("launch=%q error=%v", result.Created.LaunchID, err)
	}
}

func TestProductCreatorLaunchesRecentGameAndPersistsReturnPage(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	command.Request.ReturnTo = "/recent"
	result, err := creator.Create(t.Context(), command)
	if err != nil || result.Status != 201 || len(repository.writes) != 1 {
		t.Fatalf("status=%d writes=%d error=%v", result.Status, len(repository.writes), err)
	}
	if repository.writes[0].Command.Request.ReturnTo != "/recent" {
		t.Fatalf("return page=%q", repository.writes[0].Command.Request.ReturnTo)
	}
}

func TestProductCreatorRejectsInvalidReturnPageBeforeReadingOrWriting(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	command.Request.ReturnTo = "/recent?redirect=https://example.invalid"
	_, err := creator.Create(t.Context(), command)
	if !errors.Is(err, ErrInvalidReturnTo) || repository.loads != 0 || repository.transactions != 0 {
		t.Fatalf("error=%v loads=%d transactions=%d", err, repository.loads, repository.transactions)
	}
}

func TestProductCreatorRejectsIncompatibleAlternateArcadeBeforeWriter(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	dat := "current-dat"
	repository.before.Source.CoreID = "mame_arcade"
	repository.before.Source.ActiveDATVersionID = &dat
	repository.before.Source.VariantID = ""
	repository.before.Source.VariantStatus = ""
	repository.before.Source.ValidationLogicalName = "puckman.zip"
	creator.environment.PrepareArcade = func(context.Context, gamevariant.Snapshot) (*gamevariant.ArcadePreparation, error) {
		return nil, gamevariant.ErrBlocked
	}
	_, err := creator.Create(t.Context(), command)
	if !errors.Is(err, ErrBlocked) || repository.transactions != 0 {
		t.Fatalf("error=%v transactions=%d", err, repository.transactions)
	}
}
