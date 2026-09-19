package tagging

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	model "retrom/internal/model/tagging"
)

const (
	boundaryAdmin = "01980000-0000-7000-8000-00000000b401"
	boundaryGame  = "01980000-0000-7000-8000-00000000f401"
	boundaryTag   = "01980000-0000-7000-8000-00000000c001"
)

type boundaryRepository struct {
	model.Repository

	scope  model.WriteScope
	writes int
}

func (repository *boundaryRepository) WithWrite(_ context.Context, work func(model.WriteScope) error) error {
	repository.writes++
	return work(repository.scope)
}

type boundaryTags struct {
	model.TagReader

	active     map[string]string
	item       model.AdminItem
	references []model.Reference
}

func (tags boundaryTags) Get(context.Context, string) (model.AdminItem, error) { return tags.item, nil }

func (tags boundaryTags) ActiveByNameKey(context.Context) (map[string]string, error) {
	return tags.active, nil
}

func (tags boundaryTags) ActiveReferences(context.Context, []string) ([]model.Reference, error) {
	return tags.references, nil
}

type boundaryRelations struct {
	model.RelationRecords

	references []model.Reference
}

func (records boundaryRelations) References(context.Context, model.Owner) ([]model.Reference, error) {
	return records.references, nil
}

type boundaryGames struct{ model.GameRecords }

func (boundaryGames) Version(context.Context, string) (int64, error) { return 2, nil }

func TestCapacityPreventsAnyTagOrAuditWrite(t *testing.T) {
	t.Parallel()
	active := make(map[string]string, model.MaxActiveTags)
	for index := range model.MaxActiveTags {
		active[fmt.Sprint(index)] = boundaryTag
	}
	repository := &boundaryRepository{scope: model.WriteScope{Tags: boundaryTags{active: active}}}
	service := New(repository, time.Now)
	if _, err := service.Create(
		t.Context(),
		boundaryAdmin,
		"新标签",
	); !errors.Is(
		err, model.ErrLimitReached,
	) {
		t.Fatalf(
			"create at capacity: %v",
			err,
		)
	}
	if _, err := service.EnsureCommonTags(
		t.Context(),
		boundaryAdmin,
	); !errors.Is(
		err, model.ErrLimitReached,
	) {
		t.Fatalf(
			"ensure at capacity: %v",
			err,
		)
	}
}

func TestStaleRenameCannotWriteOrAudit(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{
		scope: model.WriteScope{
			Tags: boundaryTags{
				item: model.AdminItem{
					TagID:   boundaryTag,
					Name:    "Old",
					Status:  model.StatusActive,
					Version: 2,
				},
			},
		},
	}
	_, err := New(repository, time.Now).Rename(t.Context(), boundaryAdmin, boundaryTag, "New", 1)
	if !errors.Is(err, model.ErrVersionConflict) {
		t.Fatalf("stale rename: %v", err)
	}
}

func TestUnchangedGameTagsDoNotAdvanceVersionsOrAudit(t *testing.T) {
	t.Parallel()
	refs := []model.Reference{{TagID: boundaryTag, Name: "Action"}}
	repository := &boundaryRepository{scope: model.WriteScope{
		Tags: boundaryTags{references: refs}, Relations: boundaryRelations{references: refs}, Games: boundaryGames{},
	}}
	result, err := New(repository, time.Now).ReplaceGameTags(t.Context(), boundaryAdmin, boundaryGame, 2, []string{boundaryTag})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 2 || !slices.Equal(result.Tags, refs) {
		t.Fatalf("unchanged tags = %#v", result)
	}
}

func TestReferenceValidationReportsEveryMissingTag(t *testing.T) {
	t.Parallel()
	missingA := "01980000-0000-7000-8000-00000000c002"
	missingB := "01980000-0000-7000-8000-00000000c003"
	reader := boundaryTags{references: []model.Reference{{TagID: boundaryTag, Name: "Action"}}}
	_, err := ValidateActiveReferences(t.Context(), reader, []string{missingB, boundaryTag, missingA})
	var invalid *model.InvalidReferencesError
	if !errors.As(err, &invalid) {
		t.Fatalf("missing tags: %v", err)
	}
	if !slices.Equal(invalid.IDs, []string{missingA, missingB}) {
		t.Fatalf("missing tag IDs = %v", invalid.IDs)
	}
}

func TestInvalidActorDoesNotOpenWriteScope(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{}
	if _, err := New(
		repository,
		time.Now,
	).Create(
		t.Context(),
		"invalid",
		"Action",
	); !errors.Is(
		err, model.ErrInvalid,
	) {
		t.Fatalf(
			"invalid actor: %v",
			err,
		)
	}
	if repository.writes != 0 {
		t.Fatalf("opened %d scopes for invalid actor", repository.writes)
	}
}
