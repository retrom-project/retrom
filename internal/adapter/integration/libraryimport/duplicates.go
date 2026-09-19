package libraryimport

import (
	"context"
	"fmt"

	libraryimportmodel "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	repository "retrom/internal/repo/libraryimport"
)

var ErrDuplicateContent = libraryimportmodel.ErrDuplicateContent

type DuplicateGame = libraryimportmodel.DuplicateGame

type DuplicateConflict = libraryimportmodel.DuplicateConflict

func findDuplicateGames(
	ctx context.Context,
	executor dbexec.Executor,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	duplicates := libraryimportmodel.NewContentDuplicates(repository.BindContentDuplicates(executor))
	games, err := duplicates.Matches(ctx, itemID, platformID)
	if err != nil {
		return nil, fmt.Errorf("read duplicate games: %w", err)
	}
	return games, nil
}

func (service *Service) DuplicateGames(
	ctx context.Context,
	itemID string,
) ([]DuplicateGame, string, error) {
	duplicates := libraryimportmodel.NewContentDuplicates(repository.BindContentDuplicates(service.database))
	games, digest, err := duplicates.Review(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("read review duplicates: %w", err)
	}
	return games, digest, nil
}
