package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type detailScopeFixture struct {
	scope ReviewReadScope
	err   error
}

func (fixture detailScopeFixture) WithRead(_ context.Context, work func(ReviewReadScope) error) error {
	if err := work(fixture.scope); err != nil {
		return err
	}
	return fixture.err
}

type detailDraftFixture struct {
	ReviewDraftReader
	head ReviewHead
	err  error
}

func (fixture detailDraftFixture) Head(context.Context, string) (ReviewHead, error) {
	return fixture.head, fixture.err
}

func TestReviewDetailChecksVisibilityBeforeChildren(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{ErrReviewNotFound, errors.New("query headline failed")} {
		service := NewReviewDetails(detailScopeFixture{scope: ReviewReadScope{Drafts: detailDraftFixture{err: cause}}})
		result, err := service.Get(t.Context(), "hidden")
		if !errors.Is(err, cause) || !reflect.DeepEqual(result, ReviewDetail{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestReviewDetailDuplicateScopeUsesPlatformType(t *testing.T) {
	t.Parallel()
	reader := &duplicateReaderStub{parts: []ContentIdentityPart{{Role: "CONTENT", SHA256: strings.Repeat("a", 64), Count: 1}}}
	scope := ReviewReadScope{Duplicates: reader}
	head := ReviewHead{
		ItemID: "item", SnapshotID: "snapshot", ContentKind: "SINGLE_FILE", PlatformID: "gba",
		PlatformInstance: ReviewPlatformInstance{ID: "instance-id"},
	}
	var result ReviewDetail
	if err := readReviewContent(t.Context(), scope, head, &result); err != nil {
		t.Fatal(err)
	}
	if len(reader.queries) != 1 || reader.queries[0].PlatformID != "gba" {
		t.Fatalf("duplicate lookup confused directory instance with platform type: %+v", reader.queries)
	}
}
