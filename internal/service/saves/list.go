package saves

import (
	"context"
	"fmt"
)

func (service *Service) List(ctx context.Context, query ListQuery) ([]ListItem, error) {
	repository, ok := service.repository.(ListRepository)
	if !ok || repository == nil {
		return nil, ErrRepositoryUnavailable
	}
	items, err := repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list saved checkpoints: %w", err)
	}
	return items, nil
}

func (service *Service) Rename(ctx context.Context, request RenameRequest) error {
	repository, ok := service.repository.(StateMutationRepository)
	if !ok || repository == nil {
		return ErrRepositoryUnavailable
	}
	if err := repository.Rename(ctx, request); err != nil {
		return fmt.Errorf("rename saved checkpoint: %w", err)
	}
	return nil
}

func (service *Service) Delete(ctx context.Context, request DeleteRequest) error {
	repository, ok := service.repository.(StateMutationRepository)
	if !ok || repository == nil {
		return ErrRepositoryUnavailable
	}
	if err := repository.Delete(ctx, request); err != nil {
		return fmt.Errorf("delete saved checkpoint: %w", err)
	}
	return nil
}
