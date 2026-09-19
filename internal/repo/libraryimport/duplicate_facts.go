package libraryimport

import (
	"context"
	"errors"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

// loadSnapshotIdentity loads fact values and computes the content identity
// digest using pure model functions. No I/O is performed in model.
func loadSnapshotIdentity(ctx context.Context, executor dbexec.Executor, itemID string) (string, error) {
	reader := BindContentDuplicates(executor)
	snapshot, err := reader.Snapshot(ctx, itemID)
	if err != nil {
		return "", err
	}
	if snapshot.Kind == "MULTI_DISC" {
		discs, err := reader.OrderedDiscs(ctx, snapshot.ID)
		if err != nil {
			return "", err
		}
		return application.ComputeDiscIdentity(discs)
	}
	parts, err := reader.IdentityParts(ctx, snapshot.ID)
	if err != nil {
		return "", err
	}
	return application.ComputeContentIdentity(parts)
}

// inspectDuplicates loads snapshot identity and published matches. Returns
// empty matches with no error when multi-disc identity is incomplete.
func inspectDuplicates(
	ctx context.Context,
	executor dbexec.Executor,
	snapshot application.ContentSnapshot,
	platformID string,
) ([]application.DuplicateGame, string, error) {
	reader := BindContentDuplicates(executor)
	var digest string
	var err error
	if snapshot.Kind == "MULTI_DISC" {
		discs, loadErr := reader.OrderedDiscs(ctx, snapshot.ID)
		if loadErr != nil {
			return nil, "", loadErr
		}
		digest, err = application.ComputeDiscIdentity(discs)
	} else {
		parts, loadErr := reader.IdentityParts(ctx, snapshot.ID)
		if loadErr != nil {
			return nil, "", loadErr
		}
		digest, err = application.ComputeContentIdentity(parts)
	}
	if errors.Is(err, application.ErrMultiDiscIncomplete) {
		return []application.DuplicateGame{}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	games, err := reader.PublishedMatches(ctx, application.DuplicateQuery{
		SnapshotID:  snapshot.ID,
		PlatformID:  platformID,
		ContentKind: snapshot.Kind,
	})
	if err != nil {
		return nil, "", fmt.Errorf("read duplicate games: %w", err)
	}
	return games, digest, nil
}
