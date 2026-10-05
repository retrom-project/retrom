package tagging

import (
	"context"
	"testing"
	"time"
)

type retriedCommonTags struct {
	Repository
	scope WriteScope
}

func (repository retriedCommonTags) WithWrite(_ context.Context, work func(WriteScope) error) error {
	if err := work(repository.scope); err != nil {
		return err
	}
	// The read-only first attempt lost serialization at commit. The next
	// snapshot contains the same tags; only that attempt may supply results.
	return work(repository.scope)
}

func TestCommonTagsRetryReturnsEachCommittedTagOnce(t *testing.T) {
	definitions, err := normalizedCommonTags()
	if err != nil {
		t.Fatal(err)
	}
	active := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		active[definition.nameKey] = boundaryTag
	}
	repository := retriedCommonTags{scope: WriteScope{
		Tags: boundaryTags{active: active, item: AdminItem{TagID: boundaryTag}},
	}}
	result, err := New(repository, time.Now).EnsureCommonTags(t.Context(), boundaryAdmin)
	if err != nil || len(result.CreatedItems) != 0 || len(result.ExistingItems) != len(definitions) {
		t.Fatalf("retried common tags: created=%d existing=%d error=%v",
			len(result.CreatedItems), len(result.ExistingItems), err)
	}
}
