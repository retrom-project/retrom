package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

var ErrDuplicateContent = errors.New("DUPLICATE_GAME_CONFIRMATION_REQUIRED")

type DuplicateGame struct {
	GameID                   string `json:"gameId"`
	Title                    string `json:"title"`
	PlatformInstanceID       string `json:"platformInstanceId"`
	PlatformInstanceName     string `json:"platformInstanceName"`
	CurrentContentRevisionID string `json:"-"`
}

type DuplicateConflict struct {
	ContentIdentityDigest string          `json:"contentIdentityDigest"`
	Games                 []DuplicateGame `json:"games"`
}

func (conflict *DuplicateConflict) Error() string { return ErrDuplicateContent.Error() }

func (conflict *DuplicateConflict) Unwrap() error { return ErrDuplicateContent }

type duplicateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func importItemContentIdentity(
	ctx context.Context,
	queryer duplicateQueryer,
	itemID string,
) (string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT source.role,
blob.sha256,
count(*)
FROM import_item_source_files source
JOIN blobs blob ON blob.id=source.blob_id
WHERE source.import_item_id=?
GROUP BY source.role,blob.sha256
ORDER BY source.role,blob.sha256
`, itemID)
	if err != nil {
		return "", fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	defer func() { _ = rows.Close() }()
	digest := sha256.New()
	_, _ = digest.Write([]byte("RETROM_CONTENT_IDENTITY_V1\x00"))
	partCount := 0
	for rows.Next() {
		var role, blobSHA string
		var count int64
		if err := rows.Scan(&role, &blobSHA, &count); err != nil || role == "" || len(blobSHA) != 64 || count < 1 {
			return "", ErrInvalid
		}
		_, _ = fmt.Fprintf(digest, "%s\x00%s\x00%d\x00", role, blobSHA, count)
		partCount++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	if partCount == 0 {
		return "", ErrInvalid
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func findDuplicateGames(
	ctx context.Context,
	queryer duplicateQueryer,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT game.id,
metadata.title,
instance.id,
instance.name,
game.current_content_revision_id
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
WHERE game.status='PUBLISHED'
AND instance.platform_id=?
AND NOT EXISTS (
  SELECT 1 FROM (
    SELECT existing.role,existing.blob_id,count(*) AS file_count
    FROM game_content_files existing
    WHERE existing.game_content_revision_id=game.current_content_revision_id
    GROUP BY existing.role,existing.blob_id
    EXCEPT
    SELECT incoming.role,incoming.blob_id,count(*) AS file_count
    FROM import_item_source_files incoming
    WHERE incoming.import_item_id=?
    GROUP BY incoming.role,incoming.blob_id
  ) existing_difference
)
AND NOT EXISTS (
  SELECT 1 FROM (
    SELECT incoming.role,incoming.blob_id,count(*) AS file_count
    FROM import_item_source_files incoming
    WHERE incoming.import_item_id=?
    GROUP BY incoming.role,incoming.blob_id
    EXCEPT
    SELECT existing.role,existing.blob_id,count(*) AS file_count
    FROM game_content_files existing
    WHERE existing.game_content_revision_id=game.current_content_revision_id
    GROUP BY existing.role,existing.blob_id
  ) incoming_difference
)
ORDER BY game.created_at_ms,game.id
`, platformID, itemID, itemID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	defer func() { _ = rows.Close() }()
	games := make([]DuplicateGame, 0)
	for rows.Next() {
		var game DuplicateGame
		if err := rows.Scan(
			&game.GameID,
			&game.Title,
			&game.PlatformInstanceID,
			&game.PlatformInstanceName,
			&game.CurrentContentRevisionID,
		); err != nil {
			return nil, fmt.Errorf("libraryimport/duplicate: %w", err)
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/duplicate: %w", err)
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
	var platformID string
	if err := service.database.QueryRowContext(ctx, `
SELECT instance.platform_id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&platformID); err != nil {
		return nil, "", fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	digest, err := importItemContentIdentity(ctx, service.database, itemID)
	if err != nil {
		return nil, "", err
	}
	games, err := findDuplicateGames(ctx, service.database, itemID, platformID)
	return games, digest, err
}
