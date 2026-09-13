package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

var ErrDuplicateContent = application.ErrDuplicateContent

type DuplicateGame = application.DuplicateGame

type DuplicateConflict = application.DuplicateConflict

func importItemContentIdentity(ctx context.Context, executor dbexec.Executor, itemID string) (string, error) {
	digest, err := application.NewContentDuplicates(repository.BindContentDuplicates(executor)).Identity(ctx, itemID)
	if err != nil {
		return "", fmt.Errorf("read content identity: %w", err)
	}
	return digest, nil
}

func findDuplicateGames(
	ctx context.Context,
	executor dbexec.Executor,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	duplicates := application.NewContentDuplicates(repository.BindContentDuplicates(executor))
	games, err := duplicates.Matches(ctx, itemID, platformID)
	if err != nil {
		return nil, fmt.Errorf("read duplicate games: %w", err)
	}
	return games, nil
}

func claimContentIdentity(ctx context.Context, transaction *sql.Tx, platformID, digest string, now int64) error {
	writer := repository.BindReviewApproval(transaction).Decisions
	if err := writer.ClaimIdentity(ctx, platformID, digest, now); err != nil {
		return fmt.Errorf("claim content identity: %w", err)
	}
	return nil
}

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
