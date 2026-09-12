package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

func (service *Service) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	creation := importservice.NewCreation(
		importpersistence.NewCreation(
			service.database,
		),
		configuredSources{
			service.roots,
		},
		service.now,
	)
	result, err := creation.Create(ctx, request, actorID)
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
) (importservice.RootSelection, error) {
	if err := ctx.Err(); err != nil {
		return importservice.RootSelection{}, fmt.Errorf("select source: %w", err)
	}
	if err := ValidateRootID(rootID); err != nil {
		return importservice.RootSelection{}, err
	}
	root, ok := sources.roots[rootID]
	if !ok {
		return importservice.RootSelection{}, ErrRootNotFound
	}
	if err := ValidateRelativePath(relativePath); err != nil {
		return importservice.RootSelection{}, err
	}
	directory, err := openSelectedDirectory(root.path, relativePath)
	if err != nil {
		return importservice.RootSelection{}, ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return importservice.RootSelection{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}
