package libraryimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

var ErrDuplicateContent = application.ErrDuplicateContent

type DuplicateGame = application.DuplicateGame

type DuplicateConflict = application.DuplicateConflict

func (service *Service) DuplicateGames(
	ctx context.Context,
	itemID string,
) ([]DuplicateGame, string, error) {
	duplicates := application.NewContentDuplicates(repository.BindContentDuplicates(service.database))
	games, digest, err := duplicates.Review(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("read review duplicates: %w", err)
	}
	return games, digest, nil
}
