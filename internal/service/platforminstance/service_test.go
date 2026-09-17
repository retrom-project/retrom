package platforminstance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type boundaryRepository struct {
	Repository
	scope  WriteScope
	writes int
}

func (repository *boundaryRepository) CommitWrite(_ context.Context, work func(WriteScope) error) error {
	repository.writes++
	return work(repository.scope)
}

type unavailableCore struct{ Reader }

func (unavailableCore) CoreEnabled(context.Context, string, string) (bool, error) { return false, nil }

func TestInvalidCreateNeverOpensTransaction(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{}
	service := New(repository, time.Now)
	for _, input := range []CreateInput{{Name: ""}, {Name: " leading"}, {Name: "Valid", Description: "\x00"}} {
		if _, err := service.Create(t.Context(), AuditActor{}, input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid input: %v", err)
		}
	}
	if repository.writes != 0 {
		t.Fatalf("invalid input opened %d transactions", repository.writes)
	}
}

func TestInvalidPatchNeverOpensTransaction(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{}
	service := New(repository, time.Now)
	name := ""
	for _, input := range []PlatformInstancePatch{
		{ExpectedVersion: 1, Name: &name},
		{ID: "directory", ExpectedVersion: 0, Enabled: boolPointer(true)},
		{ID: "directory", ExpectedVersion: 1},
	} {
		if _, err := service.Patch(t.Context(), input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid patch: %v", err)
		}
	}
	if repository.writes != 0 {
		t.Fatalf("invalid patch opened %d transactions", repository.writes)
	}
}

func TestUnavailableCoreCannotCreateDirectory(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{scope: WriteScope{Reader: unavailableCore{}}}
	service := New(repository, time.Now)
	if _, err := service.Create(t.Context(), AuditActor{}, CreateInput{Name: "Library", PlatformID: "gba", DefaultCoreID: "disabled"}); !errors.Is(err, ErrDefaultCoreInvalid) {
		t.Fatalf("unavailable core: %v", err)
	}
	if repository.writes != 1 {
		t.Fatalf("transaction count = %d", repository.writes)
	}
}

func TestSlugSelectionKeepsReservedNames(t *testing.T) {
	t.Parallel()
	slug, err := NextSlug(SlugBase("My Library", "gba"), []string{"my-library", "my-library-2", "my-library-4"})
	if err != nil {
		t.Fatal(err)
	}
	if slug != "my-library-3" {
		t.Fatalf("slug = %q", slug)
	}
}

func TestCoreImpactProjectionClassifiesVariantStates(t *testing.T) {
	t.Parallel()
	blockedCode := "LAUNCH_CORE_UNAVAILABLE"
	result := projectCoreImpact("gba-directory", "mgba", CoreImpactFacts{
		PlatformInstanceVersion: 3,
		ProviderID:              "provider",
		TargetID:                "target",
		BundleSHA256:            "bundle",
		Games: []CoreImpactGame{
			{GameID: "ready", GameVersion: 2, VariantID: stringPointer("variant-ready"), VariantStatus: stringPointer("READY")},
			{GameID: "blocked", GameVersion: 4, VariantID: stringPointer("variant-blocked"), VariantStatus: stringPointer("BLOCKED"), TargetCompatibilityCode: &blockedCode},
			{GameID: "pending", GameVersion: 1},
		},
	})
	if result.Counts["ready"] != 1 || result.Counts["blocked"] != 1 || result.Counts["needsValidation"] != 1 {
		t.Fatalf("impact counts = %#v", result.Counts)
	}
	if len(result.Items) != 3 || result.Items[0].Status != "READY" || result.Items[1].Status != "BLOCKED" || result.Items[2].Status != "NEEDS_VALIDATION" {
		t.Fatalf("impact items = %#v", result.Items)
	}
	if result.Impact.PlatformInstanceVersion != 3 || result.Impact.CoreID != "mgba" {
		t.Fatalf("impact = %#v", result.Impact)
	}
}

func boolPointer(value bool) *bool { return &value }
