package launch

import (
	"context"
	"errors"
	model "retrom/internal/model/launch"
	"testing"
)

type productCreationMemory struct{ failure error }

func (repository *productCreationMemory) Replay(context.Context, model.ProductCreateCommand) (model.ProductReceipt, bool, error) {
	return model.ProductReceipt{}, false, nil
}

func (repository *productCreationMemory) Snapshot(context.Context, model.ProductCreateCommand) (model.ProductSnapshot, error) {
	return model.ProductSnapshot{}, repository.failure
}

func (repository *productCreationMemory) WithCreation(context.Context, func(model.ProductCreationScope) error) error {
	return repository.failure
}

func TestProductCreatorRetainsSnapshotFailure(t *testing.T) {
	cause := errors.New("product snapshot unavailable")
	creator := NewProductCreator(&productCreationMemory{failure: cause}, nil, nil, model.ProductEnvironment{})
	result, err := creator.Create(t.Context(), model.ProductCreateCommand{ProfileID: "profile", Request: model.CreateRequest{GameID: "game", ReturnTo: "/games/game"}})
	if !errors.Is(err, cause) || result.Created.LaunchID != "" {
		t.Fatalf("launch=%q error=%v", result.Created.LaunchID, err)
	}
}
