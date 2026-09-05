package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"retrom/internal/multidisc"
)

var (
	ErrDuplicateContent    = errors.New("DUPLICATE_GAME_CONFIRMATION_REQUIRED")
	errMultiDiscIncomplete = errors.New("MULTI_DISC_IDENTITY_INCOMPLETE")
)

type DuplicateGame struct {
	GameID               string `json:"gameId"`
	Title                string `json:"title"`
	PlatformInstanceID   string `json:"platformInstanceId"`
	PlatformInstanceName string `json:"platformInstanceName"`
}

type DuplicateConflict struct {
	ContentIdentityDigest string          `json:"contentIdentityDigest"`
	Games                 []DuplicateGame `json:"games"`
}

func (conflict *DuplicateConflict) Error() string { return ErrDuplicateContent.Error() }

func (conflict *DuplicateConflict) Unwrap() error { return ErrDuplicateContent }

type duplicateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func importItemContentIdentity(
	ctx context.Context,
	queryer duplicateQueryer,
	itemID string,
) (string, error) {
	var snapshotID, contentKind string
	if err := queryer.QueryRowContext(ctx, `
SELECT snapshot.id,snapshot.content_kind
FROM import_item_source_snapshots snapshot
WHERE snapshot.id=COALESCE(
  (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
  (SELECT initial.id FROM import_item_source_snapshots initial
   WHERE initial.import_item_id=? AND initial.created_by='IDENTIFICATION')
)
`, itemID, itemID).Scan(&snapshotID, &contentKind); err != nil {
		return "", ErrInvalid
	}
	if contentKind == multidisc.ContentKind {
		return multiDiscContentIdentity(ctx, queryer, snapshotID)
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT source.role,
blob.sha256,
count(*)
FROM import_item_source_snapshot_files source
JOIN blobs blob ON blob.id=source.blob_id
WHERE source.source_snapshot_id=COALESCE(
  (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
  (SELECT snapshot.id FROM import_item_source_snapshots snapshot
   WHERE snapshot.import_item_id=? AND snapshot.created_by='IDENTIFICATION')
)
GROUP BY source.role,blob.sha256
ORDER BY source.role,blob.sha256
`, itemID, itemID)
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

func multiDiscContentIdentity(
	ctx context.Context,
	queryer duplicateQueryer,
	snapshotID string,
) (string, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT entry.state,blob.sha256
FROM import_item_multidisc_entries entry
LEFT JOIN blobs blob ON blob.id=entry.blob_id
WHERE entry.source_snapshot_id=?
ORDER BY entry.ordinal
`, snapshotID)
	if err != nil {
		return "", fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	defer func() { _ = rows.Close() }()
	discDigests := make([]string, 0, multidisc.MaxDiscs)
	for rows.Next() {
		var state string
		var digest sql.NullString
		if err := rows.Scan(&state, &digest); err != nil {
			return "", fmt.Errorf("libraryimport/duplicate: %w", err)
		}
		if state != "PRESENT" || !digest.Valid {
			return "", errMultiDiscIncomplete
		}
		discDigests = append(discDigests, digest.String)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	identity, err := multidisc.ContentIdentity(discDigests)
	if err != nil {
		return "", fmt.Errorf("libraryimport/duplicate: %w", err)
	}
	return identity, nil
}

func findDuplicateGames(
	ctx context.Context,
	queryer duplicateQueryer,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	var contentKind string
	if err := queryer.QueryRowContext(ctx, `
SELECT snapshot.content_kind
FROM import_item_source_snapshots snapshot
WHERE snapshot.id=COALESCE(
  (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
  (SELECT initial.id FROM import_item_source_snapshots initial
   WHERE initial.import_item_id=? AND initial.created_by='IDENTIFICATION')
)
`, itemID, itemID).Scan(&contentKind); err != nil {
		return nil, ErrInvalid
	}
	if contentKind == multidisc.ContentKind {
		return findOrderedMultiDiscDuplicates(ctx, queryer, itemID, platformID)
	}
	return findUnorderedDuplicates(ctx, queryer, itemID, platformID)
}

func findUnorderedDuplicates(
	ctx context.Context,
	queryer duplicateQueryer,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	return queryDuplicateGames(ctx, queryer, `
SELECT game.id,
game.title,
instance.id,
instance.name
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.status='PUBLISHED'
AND instance.platform_id=?
AND NOT EXISTS (
  SELECT 1 FROM (
    SELECT existing.role,existing.blob_id,count(*) AS file_count
    FROM game_files existing
    WHERE existing.game_id=game.id
    GROUP BY existing.role,existing.blob_id
    EXCEPT
    SELECT incoming.role,incoming.blob_id,count(*) AS file_count
    FROM import_item_source_snapshot_files incoming
    WHERE incoming.source_snapshot_id=COALESCE(
      (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
      (SELECT snapshot.id FROM import_item_source_snapshots snapshot
       WHERE snapshot.import_item_id=? AND snapshot.created_by='IDENTIFICATION')
    )
    GROUP BY incoming.role,incoming.blob_id
  ) existing_difference
)
AND NOT EXISTS (
  SELECT 1 FROM (
    SELECT incoming.role,incoming.blob_id,count(*) AS file_count
    FROM import_item_source_snapshot_files incoming
    WHERE incoming.source_snapshot_id=COALESCE(
      (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
      (SELECT snapshot.id FROM import_item_source_snapshots snapshot
       WHERE snapshot.import_item_id=? AND snapshot.created_by='IDENTIFICATION')
    )
    GROUP BY incoming.role,incoming.blob_id
    EXCEPT
    SELECT existing.role,existing.blob_id,count(*) AS file_count
    FROM game_files existing
    WHERE existing.game_id=game.id
    GROUP BY existing.role,existing.blob_id
  ) incoming_difference
)
ORDER BY game.created_at_ms,game.id
`, platformID, itemID, itemID, itemID, itemID)
}

func findOrderedMultiDiscDuplicates(
	ctx context.Context,
	queryer duplicateQueryer,
	itemID, platformID string,
) ([]DuplicateGame, error) {
	return queryDuplicateGames(ctx, queryer, `
SELECT game.id,game.title,instance.id,instance.name
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.status='PUBLISHED' AND instance.platform_id=?
AND game.content_kind='MULTI_DISC'
AND NOT EXISTS(
  SELECT incoming.sort_order,incoming.blob_id
  FROM import_item_source_snapshot_files incoming
  WHERE incoming.source_snapshot_id=COALESCE(
    (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
    (SELECT initial.id FROM import_item_source_snapshots initial
     WHERE initial.import_item_id=? AND initial.created_by='IDENTIFICATION')
  ) AND incoming.role='DISC'
  EXCEPT
  SELECT existing.sort_order,existing.blob_id FROM game_files existing
  WHERE existing.game_id=game.id AND existing.role='DISC'
)
AND NOT EXISTS(
  SELECT existing.sort_order,existing.blob_id FROM game_files existing
  WHERE existing.game_id=game.id AND existing.role='DISC'
  EXCEPT
  SELECT incoming.sort_order,incoming.blob_id
  FROM import_item_source_snapshot_files incoming
  WHERE incoming.source_snapshot_id=COALESCE(
    (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
    (SELECT initial.id FROM import_item_source_snapshots initial
     WHERE initial.import_item_id=? AND initial.created_by='IDENTIFICATION')
  ) AND incoming.role='DISC'
)
ORDER BY game.created_at_ms,game.id
`, platformID, itemID, itemID, itemID, itemID)
}

func queryDuplicateGames(
	ctx context.Context,
	queryer duplicateQueryer,
	query string,
	arguments ...any,
) ([]DuplicateGame, error) {
	rows, err := queryer.QueryContext(ctx, query, arguments...)
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
	if errors.Is(err, errMultiDiscIncomplete) {
		return []DuplicateGame{}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	games, err := findDuplicateGames(ctx, service.database, itemID, platformID)
	return games, digest, err
}
