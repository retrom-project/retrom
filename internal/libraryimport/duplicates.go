package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"retrom/internal/dbexec"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

var (
	ErrDuplicateContent    = errors.New("DUPLICATE_GAME_CONFIRMATION_REQUIRED")
	errMultiDiscIncomplete = application.ErrMultiDiscIncomplete
)

type DuplicateGame = application.DuplicateGame

type DuplicateConflict struct {
	ContentIdentityDigest string          `json:"contentIdentityDigest"`
	Games                 []DuplicateGame `json:"games"`
}

func (conflict *DuplicateConflict) Error() string { return ErrDuplicateContent.Error() }

func (conflict *DuplicateConflict) Unwrap() error { return ErrDuplicateContent }

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

func claimContentIdentity(
	ctx context.Context,
	transaction *sql.Tx,
	platformID, digest string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_identity_claims(platform_id,content_identity_digest,created_at_ms)
VALUES(?,?,?)
ON CONFLICT(platform_id,content_identity_digest) DO NOTHING
`, platformID, digest, now); err != nil {
		return fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	return nil
}

func duplicateIDs(games []DuplicateGame) []string {
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.GameID)
	}
	sort.Strings(ids)
	return ids
}

func sameDuplicateIDs(games []DuplicateGame, acknowledged []string) bool {
	if len(games) != len(acknowledged) {
		return false
	}
	want := duplicateIDs(games)
	got := append([]string(nil), acknowledged...)
	sort.Strings(got)
	for index := range want {
		if want[index] != got[index] || (index > 0 && got[index] == got[index-1]) {
			return false
		}
	}
	return true
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
