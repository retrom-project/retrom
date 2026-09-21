package gamemove

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/corevalidation"
)

type memoryRepository struct {
	subject        ImpactSubject
	variant        VariantState
	variantFound   bool
	variantErr     error
	jobState       string
	runID          string
	runFound       bool
	candidates     []CandidateRecord
	updated        bool
	updateErr      error
	auditErr       error
	updatedGameID  string
	updatedTarget  string
	updatedVersion int64
	updatedAt      int64
	audit          AuditEvent
}

func (memory *memoryRepository) ImpactSubject(context.Context, string, string) (ImpactSubject, error) {
	return memory.subject, nil
}

func (memory *memoryRepository) Variant(context.Context, VariantQuery) (VariantState, bool, error) {
	return memory.variant, memory.variantFound, memory.variantErr
}

func (memory *memoryRepository) QueuedJobState(context.Context, string) (string, error) {
	return memory.jobState, nil
}

func (memory *memoryRepository) LatestScrapeRun(context.Context, string) (string, bool, error) {
	return memory.runID, memory.runFound, nil
}

func (memory *memoryRepository) ScrapeCandidates(context.Context, string) ([]CandidateRecord, error) {
	return memory.candidates, nil
}

func (memory *memoryRepository) WithMove(_ context.Context, work func(MoveScope) error) error {
	return work(memory)
}

func (memory *memoryRepository) UpdateGame(
	_ context.Context, gameID, targetID string, expectedVersion, nowMS int64,
) (bool, error) {
	memory.updatedGameID = gameID
	memory.updatedTarget = targetID
	memory.updatedVersion = expectedVersion
	memory.updatedAt = nowMS
	return memory.updated, memory.updateErr
}

func (memory *memoryRepository) Audit(_ context.Context, event AuditEvent) error {
	memory.audit = event
	return memory.auditErr
}

type memoryValidation struct {
	snapshot corevalidation.Snapshot
	calls    int
}

func (memory *memoryValidation) ResolveBIOS(
	context.Context, string, string, string,
) (corevalidation.Snapshot, string, string, error) {
	memory.calls++
	return memory.snapshot, "READY", "READY", nil
}

func testSnapshot() corevalidation.Snapshot {
	return corevalidation.Snapshot{
		SchemaVersion: corevalidation.SnapshotSchemaVersion,
		Kind:          corevalidation.SnapshotKindStatic,
		BIOS:          []corevalidation.BIOSDependency{},
	}
}

func testSubject() ImpactSubject {
	return ImpactSubject{
		GameID:                   "game",
		SourcePlatformInstanceID: "source",
		SourcePlatformID:         "gbc",
		ContentLogicalName:       "game.gbc",
		GameVersion:              3,
		TargetPlatformInstanceID: "target",
		TargetPlatformID:         "gbc",
		TargetCoreID:             "core",
		TargetPlatformVersion:    8,
		TargetProviderID:         "provider",
		TargetID:                 "runtime-target",
	}
}

func TestPreviewNormalizesPendingVariantAndBuildsBlocker(t *testing.T) {
	repository := &memoryRepository{
		subject:      testSubject(),
		variant:      VariantState{Status: "BLOCKED", CompatibilityCode: "VALIDATION_PENDING"},
		variantFound: true,
	}
	validation := &memoryValidation{snapshot: testSnapshot()}
	service := New(repository, validation)

	impact, err := service.Preview(context.Background(), PreviewRequest{
		GameID: "game", TargetPlatformInstanceID: "target", ExpectedVersion: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if impact.VariantStatus != "NEEDS_VALIDATION" || len(impact.BlockerCodes) != 1 ||
		impact.BlockerCodes[0] != "VARIANT_VALIDATION_REQUIRED" || impact.ValidationInputDigest == "" {
		t.Fatalf("preview impact = %#v", impact)
	}
	if validation.calls != 1 {
		t.Fatalf("validation calls = %d, want 1", validation.calls)
	}
}

func TestPreviewRejectsStaleSubjectBeforeResolvingValidation(t *testing.T) {
	repository := &memoryRepository{subject: testSubject()}
	validation := &memoryValidation{snapshot: testSnapshot()}
	service := New(repository, validation)

	_, err := service.Preview(context.Background(), PreviewRequest{
		GameID: "game", TargetPlatformInstanceID: "target", ExpectedVersion: 4,
	})
	if !errors.Is(err, ErrImpactStale) {
		t.Fatalf("stale preview error = %v", err)
	}
	if validation.calls != 0 {
		t.Fatalf("validation calls = %d, want 0", validation.calls)
	}
}

func TestMoveUpdatesGameAndAuditsWithinOneScope(t *testing.T) {
	repository := &memoryRepository{updated: true}
	service := New(repository, &memoryValidation{snapshot: testSnapshot()})
	impact := Impact{
		Action: "MOVE_GAME", GameID: "game", GameVersion: 3,
		SourcePlatformInstanceID: "source", TargetPlatformInstanceID: "target",
		TargetCoreID: "core", TargetProviderID: "provider", TargetID: "runtime-target",
		VariantStatus: "READY",
	}

	result, err := service.Move(context.Background(), MoveRequest{
		GameID: "game", TargetPlatformInstanceID: "target", ExpectedVersion: 3, NowMS: 100,
		Impact: impact, Actor: AuditActor{Kind: "SYSTEM", Label: "release-setup", RequestID: "request"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 4 || result.PlatformInstanceID != "target" || result.UpdatedAtMS != 100 {
		t.Fatalf("move result = %#v", result)
	}
	if repository.updatedGameID != "game" || repository.updatedTarget != "target" ||
		repository.updatedVersion != 3 || repository.updatedAt != 100 {
		t.Fatalf("update = %q %q v%d at%d", repository.updatedGameID, repository.updatedTarget, repository.updatedVersion, repository.updatedAt)
	}
	if repository.audit.Action != "GAME_MOVED" || repository.audit.ResourceID != "game" ||
		repository.audit.Actor.RequestID != "request" {
		t.Fatalf("audit = %#v", repository.audit)
	}
}

func TestMoveMapsOptimisticLockFailure(t *testing.T) {
	repository := &memoryRepository{updated: false}
	service := New(repository, &memoryValidation{snapshot: testSnapshot()})
	_, err := service.Move(context.Background(), MoveRequest{
		GameID: "game", TargetPlatformInstanceID: "target", ExpectedVersion: 3, NowMS: 100,
		Impact: Impact{
			Action: "MOVE_GAME", GameID: "game", GameVersion: 3,
			SourcePlatformInstanceID: "source", TargetPlatformInstanceID: "target",
			TargetCoreID: "core", TargetProviderID: "provider", TargetID: "runtime-target",
		},
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("move conflict error = %v", err)
	}
	if repository.audit.ID != "" {
		t.Fatal("move wrote audit after optimistic lock failure")
	}
}

func TestScrapeCandidatesReturnsEmptyProjectionWhenNoCompletedRun(t *testing.T) {
	repository := &memoryRepository{}
	service := New(repository, &memoryValidation{snapshot: testSnapshot()})
	result, err := service.ScrapeCandidates(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID != nil || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty scrape result = %#v", result)
	}
}
