package saves

import (
	"context"
	"fmt"

	model "retrom/internal/model/saves"
)

func (service *Service) List(ctx context.Context, query model.ListQuery) ([]model.ListItem, error) {
	repository, ok := service.repository.(model.ListRepository)
	if !ok || repository == nil {
		return nil, model.ErrRepositoryUnavailable
	}
	items, err := repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list saved checkpoints: %w", err)
	}
	return items, nil
}

func (service *Service) Rename(ctx context.Context, request model.RenameRequest) error {
	repository, ok := service.repository.(model.StateMutationRepository)
	if !ok || repository == nil {
		return model.ErrRepositoryUnavailable
	}
	if err := repository.Rename(ctx, request); err != nil {
		return fmt.Errorf("rename saved checkpoint: %w", err)
	}
	return nil
}

func (service *Service) Delete(ctx context.Context, request model.DeleteRequest) error {
	repository, ok := service.repository.(model.StateMutationRepository)
	if !ok || repository == nil {
		return model.ErrRepositoryUnavailable
	}
	if err := repository.Delete(ctx, request); err != nil {
		return fmt.Errorf("delete saved checkpoint: %w", err)
	}
	return nil
}
