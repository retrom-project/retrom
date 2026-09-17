package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	model "retrom/internal/model/libraryimport"
)

type detailScopeFixture struct {
	scope model.ReviewReadScope
	err   error
}

func (fixture detailScopeFixture) WithRead(_ context.Context, work func(model.ReviewReadScope) error) error {
	if err := work(fixture.scope); err != nil {
		return err
	}
	return fixture.err
}

type detailDraftFixture struct {
	model.ReviewDraftReader
	head model.ReviewHead
	err  error
}

func (fixture detailDraftFixture) Head(context.Context, string) (model.ReviewHead, error) {
	return fixture.head, fixture.err
}

func TestReviewDetailChecksVisibilityBeforeChildren(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{model.ErrReviewNotFound, errors.New("query headline failed")} {
		service := NewReviewDetails(detailScopeFixture{scope: model.ReviewReadScope{Drafts: detailDraftFixture{err: cause}}})
		result, err := service.Get(t.Context(), "hidden")
		if !errors.Is(err, cause) || !reflect.DeepEqual(result, model.ReviewDetail{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestReviewDetailDuplicateScopeUsesPlatformType(t *testing.T) {
	t.Parallel()
	reader := &duplicateReaderStub{parts: []model.ContentIdentityPart{{Role: "CONTENT", SHA256: strings.Repeat("a", 64), Count: 1}}}
	scope := model.ReviewReadScope{Duplicates: reader}
	head := model.ReviewHead{
		ItemID: "item", SnapshotID: "snapshot", ContentKind: "SINGLE_FILE", PlatformID: "gba",
		PlatformInstance: model.ReviewPlatformInstance{ID: "instance-id"},
	}
	var result model.ReviewDetail
	if err := readReviewContent(t.Context(), scope, head, &result); err != nil {
		t.Fatal(err)
	}
	if len(reader.queries) != 1 || reader.queries[0].PlatformID != "gba" {
		t.Fatalf("duplicate lookup confused directory instance with platform type: %+v", reader.queries)
	}
}
