package launch

import (
	"context"
	"errors"
	"testing"
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
