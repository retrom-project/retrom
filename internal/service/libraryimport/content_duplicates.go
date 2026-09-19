package libraryimport

import (
	"context"
	"errors"
	"fmt"

	model "retrom/internal/model/libraryimport"
)

// ContentDuplicates orchestrates content identity computation and duplicate
// detection by loading facts via the reader and delegating pure computation
// to model functions.
type ContentDuplicates struct{ reader model.ContentDuplicateReader }

// NewContentDuplicates creates a ContentDuplicates instance.
func NewContentDuplicates(reader model.ContentDuplicateReader) *ContentDuplicates {
	return &ContentDuplicates{reader: reader}
}

func (service *ContentDuplicates) Review(ctx context.Context, itemID string) ([]model.DuplicateGame, string, error) {
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
	snapshot model.ContentSnapshot,
	platformID string,
) ([]model.DuplicateGame, string, error) {
	digest, err := service.snapshotIdentity(ctx, snapshot)
	if errors.Is(err, model.ErrMultiDiscIncomplete) {
		return []model.DuplicateGame{}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	games, err := service.reader.PublishedMatches(ctx,
		model.DuplicateQuery{
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

func (service *ContentDuplicates) Matches(ctx context.Context, itemID, platformID string) ([]model.DuplicateGame, error) {
	snapshot, err := service.reader.Snapshot(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("read duplicate source snapshot: %w", err)
	}
	games, err := service.reader.PublishedMatches(ctx,
		model.DuplicateQuery{
			SnapshotID:  snapshot.ID,
			PlatformID:  platformID,
			ContentKind: snapshot.Kind,
		})
	if err != nil {
		return nil, fmt.Errorf("read duplicate games: %w", err)
	}
	return games, nil
}

func (service *ContentDuplicates) snapshotIdentity(ctx context.Context, snapshot model.ContentSnapshot) (string, error) {
	if snapshot.Kind == "MULTI_DISC" {
		discs, err := service.reader.OrderedDiscs(ctx, snapshot.ID)
		if err != nil {
			return "", fmt.Errorf("read ordered discs: %w", err)
		}
		return model.ComputeDiscIdentity(discs)
	}
	parts, err := service.reader.IdentityParts(ctx, snapshot.ID)
	if err != nil {
		return "", fmt.Errorf("read identity parts: %w", err)
	}
	return model.ComputeContentIdentity(parts)
}
