package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
)

func (service *Service) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	result, err := service.creation.Create(ctx, request, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("create server import: %w", err)
	}
	service.signal()
	return result, nil
}

type configuredSources struct{ roots map[string]Root }

func (sources configuredSources) Select(
	ctx context.Context,
	rootID, relativePath string,
) (RootSelection, error) {
	if err := ctx.Err(); err != nil {
		return RootSelection{}, fmt.Errorf("select source: %w", err)
	}
	if err := ValidateRootID(rootID); err != nil {
		return RootSelection{}, err
	}
	root, ok := sources.roots[rootID]
	if !ok {
		return RootSelection{}, ErrRootNotFound
	}
	if err := ValidateRelativePath(relativePath); err != nil {
		return RootSelection{}, err
	}
	directory, err := openSelectedDirectory(root.path, relativePath)
	if err != nil {
		return RootSelection{}, ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return RootSelection{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}
