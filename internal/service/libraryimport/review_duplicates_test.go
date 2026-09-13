package libraryimport

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type duplicateReaderStub struct {
	ContentDuplicateReader
	parts    []ContentIdentityPart
	discs    []ContentIdentityDisc
	matchErr error
	queries  []DuplicateQuery
}

func (reader *duplicateReaderStub) IdentityParts(context.Context, string) ([]ContentIdentityPart, error) {
	return reader.parts, nil
}

func (reader *duplicateReaderStub) OrderedDiscs(context.Context, string) ([]ContentIdentityDisc, error) {
	return reader.discs, nil
}

func (reader *duplicateReaderStub) PublishedMatches(_ context.Context, query DuplicateQuery) ([]DuplicateGame, error) {
	reader.queries = append(reader.queries, query)
	return []DuplicateGame{{GameID: "existing"}}, reader.matchErr
}

func TestReviewDuplicatesUseSnapshotAndClearIdentityOnMatchFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("matching query failed")
	reader := &duplicateReaderStub{parts: []ContentIdentityPart{{Role: "CONTENT", SHA256: strings.Repeat("a", 64), Count: 1}}, matchErr: failure}
	games, digest, err := NewContentDuplicates(reader).Inspect(t.Context(), ContentSnapshot{ID: "snapshot", Kind: "SINGLE_FILE"}, "gba")
	if !errors.Is(err, failure) || games != nil || digest != "" {
		t.Fatalf("partial duplicate response: %#v %q %v", games, digest, err)
	}
	if len(reader.queries) != 1 || reader.queries[0].SnapshotID != "snapshot" || reader.queries[0].PlatformID != "gba" {
		t.Fatalf("matching did not use requested identity: %#v", reader.queries)
	}
}

func TestIncompleteMultiDiscReviewHasNoIdentityOrMatches(t *testing.T) {
	t.Parallel()
	reader := &duplicateReaderStub{discs: []ContentIdentityDisc{{State: "PRESENT", SHA256: strings.Repeat("a", 64)}, {State: "MISSING"}}}
	games, digest, err := NewContentDuplicates(reader).Inspect(t.Context(), ContentSnapshot{ID: "snapshot", Kind: "MULTI_DISC"}, "psx")
	if err != nil || games == nil || len(games) != 0 || digest != "" || len(reader.queries) != 0 {
		t.Fatalf("incomplete discs matched: %#v %q %v queries=%#v", games, digest, err, reader.queries)
	}
}

func TestContentIdentityRetainsRoleAndMultiplicity(t *testing.T) {
	t.Parallel()
	reader := &duplicateReaderStub{parts: []ContentIdentityPart{{Role: "CONTENT", SHA256: strings.Repeat("a", 64), Count: 1}}}
	service := NewContentDuplicates(reader)
	snapshot := ContentSnapshot{ID: "snapshot", Kind: "SINGLE_FILE"}
	_, original, err := service.Inspect(t.Context(), snapshot, "gba")
	if err != nil || len(original) != 64 {
		t.Fatalf("original identity: %q %v", original, err)
	}
	reader.parts[0].Count = 2
	_, repeated, err := service.Inspect(t.Context(), snapshot, "gba")
	if err != nil || repeated == original {
		t.Fatalf("multiplicity ignored: %q %v", repeated, err)
	}
	reader.parts[0].Role = "COMPANION"
	_, companion, err := service.Inspect(t.Context(), snapshot, "gba")
	if err != nil || companion == repeated {
		t.Fatalf("role ignored: %q %v", companion, err)
	}
}
