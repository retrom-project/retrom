package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/adapter/files/serversource"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
)

func (source *Sources) Select(
	ctx context.Context,
	rootID, relativePath string,
) (emulationstationimportmodel.SelectedRoot, error) {
	if err := ctx.Err(); err != nil {
		return emulationstationimportmodel.SelectedRoot{}, fmt.Errorf("select EmulationStation source: %w", err)
	}
	if err := serversource.ValidateRootID(rootID); err != nil {
		return emulationstationimportmodel.SelectedRoot{}, fmt.Errorf("validate EmulationStation root: %w", err)
	}
	root, ok := source.roots[rootID]
	if !ok {
		return emulationstationimportmodel.SelectedRoot{}, serversource.ErrRootNotFound
	}
	if err := serversource.ValidateRelativePath(relativePath); err != nil {
		return emulationstationimportmodel.SelectedRoot{}, fmt.Errorf("validate EmulationStation source path: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(root.path, relativePath)
	if err != nil {
		return emulationstationimportmodel.SelectedRoot{}, fmt.Errorf(
			"open EmulationStation source: %w: %w",
			serversource.ErrRootUnavailable,
			err,
		)
	}
	if err := directory.Close(); err != nil {
		return emulationstationimportmodel.SelectedRoot{}, fmt.Errorf(
			"close EmulationStation source: %w: %w",
			serversource.ErrRootUnavailable,
			err,
		)
	}
	return emulationstationimportmodel.SelectedRoot{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}
