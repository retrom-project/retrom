package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/serversource"
	application "retrom/internal/service/emulationstationimport"
)

func (source *Sources) Select(
	ctx context.Context,
	rootID, relativePath string,
) (application.SelectedRoot, error) {
	if err := ctx.Err(); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("select EmulationStation source: %w", err)
	}
	if err := serversource.ValidateRootID(rootID); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("validate EmulationStation root: %w", err)
	}
	root, ok := source.roots[rootID]
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
