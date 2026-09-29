package gamevariant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	contentcapability "retrom/internal/content/capability"
	corevalidation "retrom/internal/core/validation"
)

type ensureMemory struct {
	validationJobMemory
	before, current   Snapshot
	variants, pending int
	failure           error
}

func (m *ensureMemory) Snapshot(context.Context, string, string) (Snapshot, error) {
	return m.before, nil
}

func (m *ensureMemory) WithEnsure(_ context.Context, work func(EnsureScope) error) error {
	if err := work(EnsureScope{Read: func(context.Context, string, string) (Snapshot, error) { return m.current, nil }, Write: m}); err != nil {
		return err
	}
	return m.failure
}

func (m *ensureMemory) CreateVariant(context.Context, VariantWrite) error { m.variants++; return nil }

func (m *ensureMemory) MarkPending(context.Context, string, int64) error { m.pending++; return nil }

type ensureProvider struct{ valid bool }

func (p ensureProvider) BundleSHA256(string, string) (string, bool) { return "bundle", p.valid }
func ensureFixture() (*Service, *ensureMemory) {
	snapshot := Snapshot{Found: true, Source: Source{
		ContentLogicalName: "game.bin", ValidationLogicalName: "game.bin", GameID: "game", CoreID: "core", ProviderID: "provider", TargetID: "target", BundleSHA256: "bundle",
		GameVersion: 1, SourceManifestDigest: "content", ContentPolicy: contentcapability.NewPolicy("SINGLE_FILE"),
	}}
	memory := &ensureMemory{before: snapshot, current: snapshot}
	service := New(memory, ensureProvider{valid: true}, func() time.Time { return time.UnixMilli(100) }, nil)
	return service, memory
}

func TestEnsureCreatesOnlyVariantAndJobWithoutDispatch(t *testing.T) {
	service, memory := ensureFixture()
	// No supervisor is installed: Ensure must leave dispatch to the caller's commit boundary.
	result, err := service.Ensure(t.Context(), "game", "core")
	if err != nil || result.Ready || result.JobID == "" || memory.variants != 1 || memory.pending != 1 || len(memory.writes) != 1 {
		t.Fatalf("result=%#v variants=%d pending=%d jobs=%d error=%v", result, memory.variants, memory.pending, len(memory.writes), err)
	}
}

func TestScheduleReloadsNewArcadeBIOSBeforeQueuing(t *testing.T) {
	_, memory := ensureFixture()
	before := memory.before
	dat, file, matched := "current-dat", "current-bios-blob", "MATCHED"
	before.Source.CoreID = "mame_arcade"
	before.Source.ProviderID = "retrom-runtime"
	before.Source.TargetID = "mame-arcade"
	before.Source.ActiveDATVersionID = &dat
	before.Source.ValidationLogicalName = "sample.zip"
	refreshed := before
	refreshed.Source.VariantID = validationFixtureID
	refreshed.Source.DATVersionID = &dat
	refreshed.ValidationBIOS = BIOSFacts{Arcade: []ArcadeBIOS{{
		State: "MISSING", CatalogPresent: true,
		Dependency: corevalidation.BIOSDependency{
			BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{
				LogicalName: "bios.zip", DeliveryKind: "BIOS_BUNDLE", RequirementMode: "REQUIRED",
			},
			FileRecord: &file, InstallationStatus: &matched,
		},
	}}}
	reloaded := false
	_, err := Schedule(t.Context(), memory, before, 100,
		func() (string, error) { return validationFixtureID, nil },
		&ArcadePreparation{Snapshot: `{"schemaVersion":1,"kind":"ARCADE"}`},
		func(context.Context, string, string) (Snapshot, error) {
			reloaded = memory.variants == 1
			return refreshed, nil
		},
	)
	if err != nil || !reloaded || len(memory.writes) != 1 {
		t.Fatalf("reload=%t jobs=%d error=%v", reloaded, len(memory.writes), err)
	}
	expected, err := Inputs(refreshed, validationFixtureID)
	if err != nil {
		t.Fatal(err)
	}
	var queued ValidationSnapshot
	if err := json.Unmarshal([]byte(memory.writes[0].SnapshotJSON), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Inputs.BIOSDependencyDigest != expected.BIOSDependencyDigest ||
		queued.Inputs.ValidationInputDigest != expected.ValidationInputDigest {
		t.Fatalf("queued validation used stale BIOS: %#v", queued.Inputs)
	}
}

func TestEnsureRejectsChangedInputsAndUnavailableProvider(t *testing.T) {
	for name, mutate := range map[string]func(*Service, *ensureMemory){
		"game":     func(_ *Service, m *ensureMemory) { m.current.Source.GameVersion++ },
		"content":  func(_ *Service, m *ensureMemory) { m.current.Source.SourceManifestDigest = "new" },
		"core":     func(_ *Service, m *ensureMemory) { m.current.Source.CoreID = "different" },
		"deleted":  func(_ *Service, m *ensureMemory) { m.current.Found = false },
		"provider": func(s *Service, _ *ensureMemory) { s.provider = ensureProvider{} },
	} {
		t.Run(name, func(t *testing.T) {
			service, memory := ensureFixture()
			mutate(service, memory)
			result, err := service.Ensure(t.Context(), "game", "core")
			if !errors.Is(err, ErrBlocked) || result.JobID != "" || memory.variants != 0 || len(memory.writes) != 0 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestEnsureDoesNotReturnUncommittedJob(t *testing.T) {
	service, memory := ensureFixture()
	memory.failure = errors.New("commit unavailable")
	result, err := service.Ensure(t.Context(), "game", "core")
	if !errors.Is(err, memory.failure) || result.JobID != "" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}
