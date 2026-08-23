package tagging

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

func batchReferences(
	ctx context.Context,
	database *sql.DB,
	query string,
	ownerIDs []string,
) (map[string][]Reference, error) {
	result := make(map[string][]Reference, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return result, nil
	}
	rows, err := database.QueryContext(ctx, query, encodedIDs(ownerIDs))
	if err != nil {
		return nil, fmt.Errorf("tagging: query owner references: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var ownerID string
		var reference Reference
		if err := rows.Scan(&ownerID, &reference.TagID, &reference.Name); err != nil {
			return nil, fmt.Errorf("tagging: scan owner references: %w", err)
		}
		result[ownerID] = append(result[ownerID], reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate owner references: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewReferences(
	ctx context.Context,
	itemIDs []string,
) (map[string][]Reference, error) {
	return batchReferences(ctx, service.database, `
SELECT draft.import_item_id,tag.id,tag.name
FROM review_draft_tags relation
JOIN review_drafts draft ON draft.id=relation.review_draft_id
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE draft.import_item_id IN (SELECT value FROM json_each(?))
ORDER BY draft.import_item_id,tag.name_key,tag.id
`, itemIDs)
}

func (service *Service) PegasusReferences(
	ctx context.Context,
	collectionIDs []string,
) (map[string][]Reference, error) {
	return batchReferences(ctx, service.database, `
SELECT relation.collection_id,tag.id,tag.name
FROM pegasus_collection_tags relation
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.collection_id IN (SELECT value FROM json_each(?))
ORDER BY relation.collection_id,tag.name_key,tag.id
`, collectionIDs)
}

func (service *Service) EmulationStationReferences(
	ctx context.Context,
	collectionIDs []string,
) (map[string][]Reference, error) {
	return batchReferences(ctx, service.database, `
SELECT relation.collection_id,tag.id,tag.name
FROM emulationstation_collection_tags relation
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.collection_id IN (SELECT value FROM json_each(?))
ORDER BY relation.collection_id,tag.name_key,tag.id
`, collectionIDs)
}

func replaceOwnerReferences(
	ctx context.Context,
	database executor,
	relationTable, ownerColumn, ownerID, actorUserID string,
	desired []Reference,
	now int64,
) ([]Reference, []Reference, error) {
	before, err := activeReferences(ctx, database, relationTable, ownerColumn, ownerID)
	if err != nil {
		return nil, nil, err
	}
	if sameReferences(before, desired) {
		return before, desired, nil
	}
	added, removed := referenceDiff(before, desired)
	for _, value := range removed {
		query := `DELETE FROM ` + relationTable + ` WHERE ` + ownerColumn + `=? AND tag_id=?`
		if _, err := database.ExecContext(ctx, query, ownerID, value.TagID); err != nil {
			return nil, nil, fmt.Errorf("tagging: remove owner tag: %w", err)
		}
	}
	for _, value := range added {
		query := `INSERT INTO ` + relationTable + `(` + ownerColumn +
			`,tag_id,assigned_by_user_id,created_at_ms) VALUES(?,?,?,?)`
		if _, err := database.ExecContext(ctx, query, ownerID, value.TagID, actorUserID, now); err != nil {
			return nil, nil, fmt.Errorf("tagging: add owner tag: %w", err)
		}
	}
	touched := append(referenceIDs(added), referenceIDs(removed)...)
	if len(touched) > 0 {
		if _, err := database.ExecContext(ctx, `
UPDATE tags SET version=version+1,updated_by_user_id=?,updated_at_ms=?
WHERE status='ACTIVE' AND id IN (SELECT value FROM json_each(?))
`, actorUserID, now, encodedIDs(touched)); err != nil {
			return nil, nil, fmt.Errorf("tagging: advance owner tag usage: %w", err)
		}
	}
	return before, desired, nil
}

func (service *Service) ValidateReferences(
	ctx context.Context,
	transaction *sql.Tx,
	tagIDs []string,
) ([]Reference, error) {
	return ValidateActiveReferences(ctx, transaction, tagIDs)
}

func (service *Service) ReplaceReviewDraftTags(
	ctx context.Context,
	transaction *sql.Tx,
	draftID string,
	tagIDs []string,
	actorUserID string,
	now int64,
) ([]Reference, []Reference, error) {
	if !ValidID(draftID) {
		return nil, nil, ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, transaction, tagIDs)
	if err != nil {
		return nil, nil, err
	}
	before, err := activeReferences(ctx, transaction, "review_draft_tags", "review_draft_id", draftID)
	if err != nil {
		return nil, nil, err
	}
	if sameReferences(before, desired) {
		return before, desired, nil
	}
	if !ValidID(actorUserID) {
		return nil, nil, ErrInvalid
	}
	return replaceOwnerReferences(
		ctx, transaction, "review_draft_tags", "review_draft_id", draftID, actorUserID, desired, now,
	)
}

func (service *Service) AssignReviewDraftTags(
	ctx context.Context,
	transaction *sql.Tx,
	draftID string,
	references []Reference,
	actorUserID string,
	now int64,
) error {
	if len(references) == 0 {
		return nil
	}
	if !ValidID(draftID) || !ValidID(actorUserID) {
		return ErrInvalid
	}
	for _, reference := range references {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_draft_tags(review_draft_id,tag_id,assigned_by_user_id,created_at_ms) VALUES(?,?,?,?)
`, draftID, reference.TagID, actorUserID, now); err != nil {
			return fmt.Errorf("tagging: assign review tag: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE tags SET version=version+1,updated_by_user_id=?,updated_at_ms=?
WHERE status='ACTIVE' AND id IN (SELECT value FROM json_each(?))
`, actorUserID, now, encodedIDs(referenceIDs(references))); err != nil {
		return fmt.Errorf("tagging: advance assigned review tags: %w", err)
	}
	return nil
}

func (service *Service) ReviewDraftReferences(
	ctx context.Context,
	transaction *sql.Tx,
	draftID string,
) ([]Reference, error) {
	return activeReferences(ctx, transaction, "review_draft_tags", "review_draft_id", draftID)
}

func (service *Service) CopyDraftTagsToGame(
	ctx context.Context,
	transaction *sql.Tx,
	draftID, gameID, actorUserID string,
	now int64,
) ([]Reference, error) {
	if !ValidID(draftID) || !ValidID(gameID) {
		return nil, ErrInvalid
	}
	references, err := activeReferences(ctx, transaction, "review_draft_tags", "review_draft_id", draftID)
	if err != nil {
		return nil, err
	}
	if len(references) == 0 {
		return references, nil
	}
	if !ValidID(actorUserID) {
		return nil, ErrInvalid
	}
	for _, reference := range references {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_tags(game_id,tag_id,assigned_by_user_id,created_at_ms) VALUES(?,?,?,?)
`, gameID, reference.TagID, actorUserID, now); err != nil {
			return nil, fmt.Errorf("tagging: copy review tag to game: %w", err)
		}
	}
	if len(references) > 0 {
		if _, err := transaction.ExecContext(ctx, `
UPDATE tags SET version=version+1,updated_by_user_id=?,updated_at_ms=?
WHERE status='ACTIVE' AND id IN (SELECT value FROM json_each(?))
`, actorUserID, now, encodedIDs(referenceIDs(references))); err != nil {
			return nil, fmt.Errorf("tagging: advance published tag usage: %w", err)
		}
	}
	return references, nil
}

func (service *Service) ReplacePegasusCollectionTags(
	ctx context.Context,
	transaction *sql.Tx,
	collectionID string,
	tagIDs []string,
	actorUserID string,
	now int64,
) ([]Reference, error) {
	if !ValidID(collectionID) || !ValidID(actorUserID) {
		return nil, ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, transaction, tagIDs)
	if err != nil {
		return nil, err
	}
	_, after, err := replaceOwnerReferences(
		ctx, transaction, "pegasus_collection_tags", "collection_id", collectionID, actorUserID, desired, now,
	)
	return after, err
}

func (service *Service) PegasusCollectionReferences(
	ctx context.Context,
	transaction *sql.Tx,
	collectionID string,
) ([]Reference, error) {
	return activeReferences(ctx, transaction, "pegasus_collection_tags", "collection_id", collectionID)
}

func (service *Service) ReplaceEmulationStationCollectionTags(
	ctx context.Context,
	transaction *sql.Tx,
	collectionID string,
	tagIDs []string,
	actorUserID string,
	now int64,
) ([]Reference, error) {
	if !ValidID(collectionID) || !ValidID(actorUserID) {
		return nil, ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, transaction, tagIDs)
	if err != nil {
		return nil, err
	}
	_, after, err := replaceOwnerReferences(
		ctx, transaction, "emulationstation_collection_tags", "collection_id",
		collectionID, actorUserID, desired, now,
	)
	return after, err
}

func (service *Service) EmulationStationCollectionReferences(
	ctx context.Context,
	transaction *sql.Tx,
	collectionID string,
) ([]Reference, error) {
	return activeReferences(
		ctx, transaction, "emulationstation_collection_tags", "collection_id", collectionID,
	)
}
