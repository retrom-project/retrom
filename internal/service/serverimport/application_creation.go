package serverimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/serverimport"

	"retrom/internal/foundation/cleanup"
)

func (service *Service) Create(
	ctx context.Context,
	request model.CreateRequest,
	actorID string,
) (model.Summary, error) {
	result, err := service.creation.Create(ctx, request, actorID)
	if err != nil {
		return model.Summary{}, fmt.Errorf("create server import: %w", err)
	}
	service.signal()
	return result, nil
}

type configuredSources struct{ roots map[string]Root }

func (sources configuredSources) Select(
	ctx context.Context,
	rootID, relativePath string,
) (model.RootSelection, error) {
	if err := ctx.Err(); err != nil {
		return model.RootSelection{}, fmt.Errorf("select source: %w", err)
	}
	if err := ValidateRootID(rootID); err != nil {
		return model.RootSelection{}, err
	}
	root, ok := sources.roots[rootID]
	if !ok {
		return model.RootSelection{}, ErrRootNotFound
	}
	if err := ValidateRelativePath(relativePath); err != nil {
		return model.RootSelection{}, err
	}
	directory, err := openSelectedDirectory(root.path, relativePath)
	if err != nil {
		return model.RootSelection{}, ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return model.RootSelection{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}
