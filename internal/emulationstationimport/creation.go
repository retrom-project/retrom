package emulationstationimport

import (
	"context"
	"fmt"

	persistence "retrom/internal/persistence/emulationstationimport"
	"retrom/internal/serversource"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) Create(ctx context.Context, request CreateRequest, userID string) (Summary, error) {
	creator := application.NewCreation(
		persistence.NewCreation(service.database),
		creationSourceSelector{roots: service.roots},
		service.now,
	)
	value, err := creator.Create(ctx, request, userID)
	if err != nil {
		return Summary{}, fmt.Errorf("create EmulationStation scan plan: %w", err)
	}
	service.signal()
	return value, nil
}

type creationSourceSelector struct{ roots map[string]Root }

func (selector creationSourceSelector) Select(
	ctx context.Context,
	rootID, relativePath string,
) (application.SelectedRoot, error) {
	if err := ctx.Err(); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("select EmulationStation source: %w", err)
	}
	if err := serversource.ValidateRootID(rootID); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("validate EmulationStation root: %w", err)
	}
	root, ok := selector.roots[rootID]
	if !ok {
		return application.SelectedRoot{}, serversource.ErrRootNotFound
	}
	if err := serversource.ValidateRelativePath(relativePath); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("validate EmulationStation source path: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(root.path, relativePath)
	if err != nil {
		return application.SelectedRoot{}, fmt.Errorf(
			"open EmulationStation source: %w: %w",
			serversource.ErrRootUnavailable,
			err,
		)
	}
	if err := directory.Close(); err != nil {
		return application.SelectedRoot{}, fmt.Errorf(
			"close EmulationStation source: %w: %w",
			serversource.ErrRootUnavailable,
			err,
		)
	}
	return application.SelectedRoot{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}
