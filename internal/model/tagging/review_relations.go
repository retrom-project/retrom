package tagging

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type ReferenceReader interface {
	References(context.Context, Owner) ([]Reference, error)
}

func ReviewDraftReferencesInScope(ctx context.Context, reader ReferenceReader, draftID string) ([]Reference, error) {
	refs, err := reader.References(ctx, Owner{Kind: OwnerReviewDraft, ID: draftID})
	return refs, relationError("review references", err)
}

func ReplaceReviewDraftTags(
	ctx context.Context,
	scope WriteScope,
	draftID string,
	ids []string,
	actorUserID string,
	now int64,
) ([]Reference, []Reference, error) {
	if !ValidID(draftID) {
		return nil, nil, ErrInvalid
	}
	desired, err := ValidateActiveReferences(ctx, scope.Tags, ids)
	if err != nil {
		return nil, nil, err
	}
	owner := Owner{Kind: OwnerReviewDraft, ID: draftID}
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, relationError("read review tags", err)
	}
	if sameReferences(before, desired) {
		return before, desired, nil
	}
	if !ValidID(actorUserID) {
		return nil, nil, ErrInvalid
	}
	return replaceOwnerReferences(ctx, scope, owner, actorUserID, desired, now)
}

func ValidateActiveReferences(ctx context.Context, reader TagReader, tagIDs []string) ([]Reference, error) {
	validated, err := ValidateIDs(tagIDs)
	if err != nil {
		return nil, err
	}
	if len(validated) == 0 {
		return []Reference{}, nil
	}
	result, err := reader.ActiveReferences(ctx, validated)
	if err != nil {
		return nil, relationError("read active references", err)
	}
	found := make(map[string]struct{}, len(result))
	for _, reference := range result {
		found[reference.TagID] = struct{}{}
	}
	if len(found) != len(validated) {
		invalid := make([]string, 0)
		for _, id := range validated {
			if _, exists := found[id]; !exists {
				invalid = append(invalid, id)
			}
		}
		sort.Strings(invalid)
		return nil, &InvalidReferencesError{IDs: invalid}
	}
	return result, nil
}

func sameReferences(left, right []Reference) bool {
	if len(left) != len(right) {
		return false
	}
	leftIDs := make([]string, len(left))
	rightIDs := make([]string, len(right))
	for index := range left {
		leftIDs[index] = left[index].TagID
		rightIDs[index] = right[index].TagID
	}
	sort.Strings(leftIDs)
	sort.Strings(rightIDs)
	return strings.Join(leftIDs, "\x00") == strings.Join(rightIDs, "\x00")
}

func referenceIDs(values []Reference) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.TagID)
	}
	return result
}

func referenceDiff(before, after []Reference) ([]Reference, []Reference) {
	added := make([]Reference, 0)
	removed := make([]Reference, 0)
	beforeByID := make(map[string]Reference, len(before))
	afterByID := make(map[string]Reference, len(after))
	for _, value := range before {
		beforeByID[value.TagID] = value
	}
	for _, value := range after {
		afterByID[value.TagID] = value
		if _, exists := beforeByID[value.TagID]; !exists {
			added = append(added, value)
		}
	}
	for _, value := range before {
		if _, exists := afterByID[value.TagID]; !exists {
			removed = append(removed, value)
		}
	}
	return added, removed
}

func replaceOwnerReferences(
	ctx context.Context,
	scope WriteScope,
	owner Owner,
	actorUserID string,
	desired []Reference,
	now int64,
) ([]Reference, []Reference, error) {
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, relationError("read owner references", err)
	}
	if sameReferences(before, desired) {
		return before, desired, nil
	}
	added, removed := referenceDiff(before, desired)
	if err := scope.Relations.Remove(ctx, owner, referenceIDs(removed)); err != nil {
		return nil, nil, relationError("remove owner tags", err)
	}
	if err := scope.Relations.Add(ctx, Assignment{
		Owner: owner, References: added, ActorUserID: actorUserID, NowMS: now,
	}); err != nil {
		return nil, nil, relationError("add owner tags", err)
	}
	touched := append(referenceIDs(added), referenceIDs(removed)...)
	if err := scope.Relations.TouchTags(ctx, actorUserID, touched, now); err != nil {
		return nil, nil, relationError("touch owner tags", err)
	}
	return before, desired, nil
}

func relationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("tagging: %s: %w", operation, err)
}
