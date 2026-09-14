package tagging

import (
	"sort"
	"strings"
)

// ReplacementPlan is the value-only description of a relation replacement.
// It deliberately contains no reader, writer, transaction, or context so it
// can be built by an application service and applied atomically by a repo.
type ReplacementPlan struct {
	Owner       Owner
	Before      []Reference
	After       []Reference
	Added       []Reference
	Removed     []Reference
	ActorUserID string
	NowMS       int64
	Changed     bool
}

// BuildReplacementPlan validates and compares already-loaded relation facts.
// The caller is responsible for loading active references; this function only
// computes the deterministic relation delta.
func BuildReplacementPlan(
	owner Owner,
	before, after []Reference,
	actorUserID string,
	now int64,
) (ReplacementPlan, error) {
	if !ValidID(owner.ID) {
		return ReplacementPlan{}, ErrInvalid
	}
	if _, err := ValidateIDs(ReferenceIDs(after)); err != nil {
		return ReplacementPlan{}, err
	}
	plan := ReplacementPlan{
		Owner: owner, Before: append([]Reference(nil), before...),
		After: append([]Reference(nil), after...), ActorUserID: actorUserID, NowMS: now,
	}
	if ReferencesEqual(before, after) {
		return plan, nil
	}
	if !ValidID(actorUserID) {
		return ReplacementPlan{}, ErrInvalid
	}
	plan.Added, plan.Removed = ReferenceDiff(before, after)
	plan.Changed = true
	return plan, nil
}

// ValidateActiveReferenceFacts validates requested IDs against an already
// loaded set of active tag records. It is the pure counterpart of the
// service-level database reader.
func ValidateActiveReferenceFacts(ids []string, result []Reference) ([]Reference, error) {
	validated, err := ValidateIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(validated) == 0 {
		return []Reference{}, nil
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

// ReferencesEqual compares relation identity as an unordered set.
func ReferencesEqual(left, right []Reference) bool {
	return sameReferences(left, right)
}

func ReferenceIDs(values []Reference) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.TagID)
	}
	return result
}

func ReferenceDiff(before, after []Reference) ([]Reference, []Reference) {
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
