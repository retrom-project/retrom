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
		Owner: owner, Before: cloneReferences(before),
		After: cloneReferences(after), ActorUserID: actorUserID, NowMS: now,
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

func cloneReferences(values []Reference) []Reference {
	if values == nil {
		return nil
	}
	return append([]Reference{}, values...)
}

// ValidateActiveReferenceFacts validates requested IDs against an already
// loaded set of active tag records. It requires exact set membership:
// every requested ID must appear exactly once in the facts, and no
// unrequested facts may be present.
func ValidateActiveReferenceFacts(ids []string, result []Reference) ([]Reference, error) {
	validated, err := ValidateIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(validated) == 0 {
		if len(result) > 0 {
			return nil, ErrInvalid
		}
		return []Reference{}, nil
	}

	// Build the requested set.
	requested := make(map[string]struct{}, len(validated))
	for _, id := range validated {
		requested[id] = struct{}{}
	}

	// Build the seen set from facts, rejecting duplicates.
	seen := make(map[string]struct{}, len(result))
	for _, ref := range result {
		if _, dup := seen[ref.TagID]; dup {
			return nil, ErrInvalid
		}
		seen[ref.TagID] = struct{}{}
	}

	// Check for missing IDs first (report all missing via InvalidReferencesError).
	missing := make([]string, 0)
	for _, id := range validated {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, &InvalidReferencesError{IDs: missing}
	}

	// After confirming no missing IDs, reject extra/unrequested facts.
	for _, ref := range result {
		if _, ok := requested[ref.TagID]; !ok {
			return nil, ErrInvalid
		}
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
