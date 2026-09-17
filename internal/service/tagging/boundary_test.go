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

type boundaryQueries struct {
	model.QueryRepository
}

type boundaryCommands struct {
	writes int

	createResult  model.AdminItem
	createError   error
	renameResult  model.AdminItem
	renameError   error
	deleteResult  model.AdminItem
	deleteImpact  model.DeleteImpact
	deleteError   error
	gameTagResult model.GameTagResult
	gameTagError  error
	commonResult  model.CommonTagsResult
	commonError   error
}

func (c *boundaryCommands) CommitCreate(_ context.Context, _ model.CreateCommand) (model.AdminItem, error) {
	c.writes++
	return c.createResult, c.createError
}

func (c *boundaryCommands) CommitRename(_ context.Context, _ model.RenameCommand) (model.AdminItem, error) {
	c.writes++
	return c.renameResult, c.renameError
}

func (c *boundaryCommands) CommitDelete(_ context.Context, _ model.DeleteCommand) (model.AdminItem, model.DeleteImpact, error) {
	c.writes++
	return c.deleteResult, c.deleteImpact, c.deleteError
}

func (c *boundaryCommands) CommitReplaceGameTags(_ context.Context, _ model.ReplaceGameTagsCommand) (model.GameTagResult, error) {
	c.writes++
	return c.gameTagResult, c.gameTagError
}

func (c *boundaryCommands) CommitEnsureCommonTags(_ context.Context, _ model.EnsureCommonTagsCommand) (model.CommonTagsResult, error) {
	c.writes++
	return c.commonResult, c.commonError
}

func idSeq() func() (string, error) {
	n := 0
	return func() (string, error) {
		n++
		return fmt.Sprintf("01980000-0000-7000-8000-%012x", n), nil
	}
}

func TestCapacityPreventsAnyTagOrAuditWrite(t *testing.T) {
	t.Parallel()
	commands := &boundaryCommands{
		createError: model.ErrLimitReached,
		commonError: model.ErrLimitReached,
	}
	service := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()})
	if _, err := service.Create(
		t.Context(),
		boundaryAdmin,
		"新标签",
	); !errors.Is(err, model.ErrLimitReached) {
		t.Fatalf("create at capacity: %v", err)
	}
	if _, err := service.EnsureCommonTags(
		t.Context(),
		boundaryAdmin,
	); !errors.Is(err, model.ErrLimitReached) {
		t.Fatalf("ensure at capacity: %v", err)
	}
}

func TestStaleRenameCannotWriteOrAudit(t *testing.T) {
	t.Parallel()
	commands := &boundaryCommands{
		renameError: model.ErrVersionConflict,
	}
	_, err := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()}).Rename(
		t.Context(), boundaryAdmin, boundaryTag, "New", 1,
	)
	if !errors.Is(err, model.ErrVersionConflict) {
		t.Fatalf("stale rename: %v", err)
	}
}

func TestUnchangedGameTagsDoNotAdvanceVersionsOrAudit(t *testing.T) {
	t.Parallel()
	refs := []model.Reference{{TagID: boundaryTag, Name: "Action"}}
	commands := &boundaryCommands{
		gameTagResult: model.GameTagResult{GameID: boundaryGame, Version: 2, Tags: refs},
	}
	result, err := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()}).ReplaceGameTags(
		t.Context(), boundaryAdmin, boundaryGame, 2, []string{boundaryTag},
	)
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
	commands := &boundaryCommands{}
	if _, err := New(
		&boundaryQueries{},
		commands,
		Options{Now: time.Now, NewID: idSeq()},
	).Create(
		t.Context(),
		"invalid",
		"Action",
	); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("invalid actor: %v", err)
	}
	if commands.writes != 0 {
		t.Fatalf("opened %d scopes for invalid actor", commands.writes)
	}
}

// boundaryTags is a test double for activeReferenceReader and tag lookup.
type boundaryTags struct {
	active     map[string]string
	item       model.AdminItem
	references []model.Reference
}

func (tags boundaryTags) Get(context.Context, string) (model.AdminItem, error) {
	return tags.item, nil
}

func (tags boundaryTags) ActiveByNameKey(context.Context) (map[string]string, error) {
	return tags.active, nil
}

func (tags boundaryTags) ActiveReferences(context.Context, []string) ([]model.Reference, error) {
	return tags.references, nil
}

func TestLayeringInvalidInputDoesNotCallCommand(t *testing.T) {
	t.Parallel()
	commands := &boundaryCommands{}
	service := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()})

	// Invalid actor
	if _, err := service.Create(t.Context(), "bad", "tag"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("bad actor create: %v", err)
	}
	// Invalid tag ID
	if _, err := service.Rename(t.Context(), boundaryAdmin, "bad", "new", 1); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("bad tag rename: %v", err)
	}
	// Invalid version
	if _, _, err := service.Delete(t.Context(), boundaryAdmin, boundaryTag, "x", 0); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("zero version delete: %v", err)
	}
	// Invalid game
	if _, err := service.ReplaceGameTags(t.Context(), boundaryAdmin, "bad", 1, []string{}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("bad game: %v", err)
	}
	// Invalid actor for ensure
	if _, err := service.EnsureCommonTags(t.Context(), "bad"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("bad actor ensure: %v", err)
	}

	if commands.writes != 0 {
		t.Fatalf("command port called %d times for invalid inputs", commands.writes)
	}
}

func TestLayeringServiceSubmitsValuesOnly(t *testing.T) {
	t.Parallel()
	commands := &boundaryCommands{
		createResult: model.AdminItem{TagID: "x", Name: "Action", Version: 1, Status: model.StatusActive},
	}
	service := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()})
	_, err := service.Create(t.Context(), boundaryAdmin, "Action")
	if err != nil {
		t.Fatal(err)
	}
	if commands.writes != 1 {
		t.Fatalf("writes = %d, want 1", commands.writes)
	}
}

func TestLayeringServicePreservesRepositoryCause(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("storage sentinel")
	commands := &boundaryCommands{createError: sentinel}
	service := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()})
	_, err := service.Create(t.Context(), boundaryAdmin, "Action")
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost sentinel: %v", err)
	}
}

func TestLayeringServiceDoesNotRetryVersionConflict(t *testing.T) {
	t.Parallel()
	commands := &boundaryCommands{renameError: model.ErrVersionConflict}
	service := New(&boundaryQueries{}, commands, Options{Now: time.Now, NewID: idSeq()})
	_, err := service.Rename(t.Context(), boundaryAdmin, boundaryTag, "New", 1)
	if !errors.Is(err, model.ErrVersionConflict) {
		t.Fatalf("rename error: %v", err)
	}
	if commands.writes != 1 {
		t.Fatalf("writes = %d, want exactly 1 (no retry)", commands.writes)
	}
}
