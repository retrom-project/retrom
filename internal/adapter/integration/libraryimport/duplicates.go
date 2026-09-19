package libraryimport

import (
	"context"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

var ErrDuplicateContent = application.ErrDuplicateContent

type DuplicateGame = application.DuplicateGame

type DuplicateConflict = application.DuplicateConflict

func findDuplicateGames(
	ctx context.Context,
	executor dbexec.Executor,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	duplicates := libraryimportservice.NewContentDuplicates(repository.BindContentDuplicates(executor))
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
	duplicates := libraryimportservice.NewContentDuplicates(repository.BindContentDuplicates(service.database))
	games, digest, err := duplicates.Review(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("read review duplicates: %w", err)
	}
	return games, digest, nil
}
