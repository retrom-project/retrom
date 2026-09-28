package libraryimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

var ErrDuplicateContent = libraryservice.ErrDuplicateContent

type DuplicateGame = libraryservice.DuplicateGame

type DuplicateConflict = libraryservice.DuplicateConflict

func (service *Service) DuplicateGames(
	ctx context.Context,
	itemID string,
) ([]DuplicateGame, string, error) {
	duplicates := libraryservice.NewContentDuplicates(repository.BindContentDuplicates(service.database))
	games, digest, err := duplicates.Review(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("read review duplicates: %w", err)
	}
	return games, digest, nil
}
