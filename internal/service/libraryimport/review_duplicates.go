package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"retrom/internal/multidisc"
)

type ContentDuplicates struct{ reader ContentDuplicateReader }

func NewContentDuplicates(reader ContentDuplicateReader) *ContentDuplicates {
	return &ContentDuplicates{reader: reader}
}

func (service *ContentDuplicates) Review(ctx context.Context, itemID string) ([]DuplicateGame, string, error) {
	platformID, err := service.reader.ReviewPlatform(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("read review duplicate platform: %w", err)
	}
	snapshot, err := service.reader.Snapshot(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("read review duplicate snapshot: %w", err)
	}
	return service.Inspect(ctx, snapshot, platformID)
}

func (service *ContentDuplicates) Inspect(
	ctx context.Context,
	snapshot ContentSnapshot,
	platformID string,
) ([]DuplicateGame, string, error) {
	digest, err := service.snapshotIdentity(ctx, snapshot)
	if errors.Is(err, ErrMultiDiscIncomplete) {
		return []DuplicateGame{}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	games, err := service.reader.PublishedMatches(ctx,
		DuplicateQuery{
			SnapshotID:  snapshot.ID,
			PlatformID:  platformID,
			ContentKind: snapshot.Kind,
		})
	if err != nil {
		return nil, "", fmt.Errorf("read duplicate games: %w", err)
	}
	return games, digest, nil
}

func (service *ContentDuplicates) Identity(ctx context.Context, itemID string) (string, error) {
	snapshot, err := service.reader.Snapshot(ctx, itemID)
	if err != nil {
		return "", fmt.Errorf("read content identity snapshot: %w", err)
	}
	return service.snapshotIdentity(ctx, snapshot)
}

func (service *ContentDuplicates) Matches(ctx context.Context, itemID, platformID string) ([]DuplicateGame, error) {
	snapshot, err := service.reader.Snapshot(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("read duplicate source snapshot: %w", err)
	}
	games, err := service.reader.PublishedMatches(ctx,
		DuplicateQuery{
			SnapshotID:  snapshot.ID,
			PlatformID:  platformID,
			ContentKind: snapshot.Kind,
		})
	if err != nil {
		return nil, fmt.Errorf("read duplicate games: %w", err)
	}
	return games, nil
}

func (service *ContentDuplicates) snapshotIdentity(ctx context.Context, snapshot ContentSnapshot) (string, error) {
	if snapshot.Kind == multidisc.ContentKind {
		return service.discIdentity(ctx, snapshot.ID)
	}
	parts, err := service.reader.IdentityParts(ctx, snapshot.ID)
	if err != nil {
		return "", fmt.Errorf("read identity parts: %w", err)
	}
	if len(parts) == 0 {
		return "", ErrInvalid
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("RETROM_CONTENT_IDENTITY_V1\x00"))
	for _, part := range parts {
		if part.Role == "" || len(part.SHA256) != 64 || part.Count < 1 {
			return "", ErrInvalid
		}
		_, _ = fmt.Fprintf(digest, "%s\x00%s\x00%d\x00", part.Role, part.SHA256, part.Count)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (service *ContentDuplicates) discIdentity(ctx context.Context, snapshotID string) (string, error) {
	discs, err := service.reader.OrderedDiscs(ctx, snapshotID)
	if err != nil {
		return "", fmt.Errorf("read ordered content identity: %w", err)
	}
	hashes := make([]string, 0, len(discs))
	for _, disc := range discs {
		if disc.State != "PRESENT" || disc.SHA256 == "" {
			return "", ErrMultiDiscIncomplete
		}
		hashes = append(hashes, disc.SHA256)
	}
	digest, err := multidisc.ContentIdentity(hashes)
	if err != nil {
		return "", fmt.Errorf("compute ordered content identity: %w", err)
	}
	return digest, nil
}
