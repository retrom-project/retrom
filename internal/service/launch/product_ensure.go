package launch

import (
	"context"
	"fmt"
)

// EnsureVariant prepares the requested core for a move without creating a launch
// or dispatching a worker before the caller has committed its own operation.
func (service *ProductCreator) EnsureVariant(
	ctx context.Context,
	gameID, coreID string,
	capabilities Capabilities,
) (Created, error) {
	if gameID == "" {
		return Created{}, ErrBlocked
	}
	command := ProductCreateCommand{
		Request: CreateRequest{GameID: gameID, CoreID: &coreID, ClientCapabilities: capabilities},
	}
	before, err := service.repository.Snapshot(ctx, command)
	if err != nil {
		return Created{}, fmt.Errorf("read move validation snapshot: %w", err)
	}
	if !before.Found {
		return Created{}, ErrBlocked
	}
	if err := service.validateProvider(before.Source, capabilities); err != nil {
		return Created{}, err
	}
	if service.environment.Now().UnixMilli() < 0 {
		return Created{}, ErrBlocked
	}
	var result Created
	err = service.repository.WithCreation(ctx, func(scope ProductCreationScope) error {
		current, err := scope.Snapshot(ctx, command)
		if err != nil {
			return fmt.Errorf("read final move validation source: %w", err)
		}
		if !sameProductInputs(before, current, true) {
			return ErrBlocked
		}
		now := service.environment.Now().UnixMilli()
		if now < 0 {
			return ErrBlocked
		}
		var queuedErr error
		result, _, queuedErr = service.schedule(ctx, scope.Validation(), current, now)
		return queuedErr
	})
	if err != nil {
		return Created{}, fmt.Errorf("ensure product variant: %w", err)
	}
	return result, nil
}
