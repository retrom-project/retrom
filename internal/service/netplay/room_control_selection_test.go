package netplay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/netplay"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/runtime/runtimecatalog"
	validation "retrom/internal/model/corevalidation"
	"retrom/internal/transport/netplay/profile"
)

type controlBIOSRepository struct{}

func (controlBIOSRepository) Catalog(context.Context, string, string) ([]corevalidation.BIOSCatalogEntry, error) {
	return nil, nil
}

func (controlBIOSRepository) BIOS(context.Context, string, string) ([]validation.BIOSRecord, error) {
	return []validation.BIOSRecord{}, nil
}

func controlRegistry(t *testing.T) *profile.Registry {
	t.Helper()
	root := filepath.Join("..", "..", "..", "data")
	bindings, err := os.ReadFile(filepath.Join(root, "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(bindings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, profile.ManifestRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := profile.ParseRegistry(raw, &dependencies.Set{RuntimeCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func controlSelectionFixture(t *testing.T) (*RoomControl, *roomControlMemory, *eligibilityMemory) {
	t.Helper()
	registry := controlRegistry(t)
	selected, ok := registry.Profile("fceumm-423-v1")
	if !ok {
		t.Fatal("missing registered profile")
	}
	row := model.EligibilityRow{VariantID: "variant", CoreID: "fceumm", ProviderID: "emulatorjs", TargetID: "fceumm", PlatformID: "nes", ContentKind: "SINGLE_FILE", LogicalName: "game.nes", BundleSHA256: strings.Repeat("a", 64), SourceManifestDigest: strings.Repeat("b", 64), DependencyJSON: `{"schemaVersion":1,"kind":"STATIC","bios":[]}`}
	_, digest, err := registry.CanonicalProfile(profile.CanonicalProfileInput{ManifestProfile: selected, BundleSHA256: row.BundleSHA256, SourceManifestDigest: row.SourceManifestDigest, DependencySnapshotJSON: row.DependencyJSON})
	if err != nil {
		t.Fatal(err)
	}
	before := controlSnapshot()
	before.Selection = &model.RoomSelection{GameID: "game", VariantID: "variant", ProfileID: selected.ID, Digest: digest, MaxPlayers: 2}
	eligibility := &eligibilityMemory{rows: map[string][]model.EligibilityRow{"game": {row}}}
	memory := &roomControlMemory{before: before, result: model.Room{RoomID: "room"}, eligibility: eligibility, bios: controlBIOSRepository{}}
	return NewRoomControl(memory, registry, time.Hour, time.Hour, time.Now), memory, eligibility
}

func TestReadyRequiresExactFrozenVariantAndCanonicalDigest(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*model.EligibilityRow)
		want   error
	}{
		{"same snapshot", func(*model.EligibilityRow) {}, nil},
		{"new source", func(row *model.EligibilityRow) { row.SourceManifestDigest = strings.Repeat("c", 64) }, model.ErrProfileStale},
		{"new bundle", func(row *model.EligibilityRow) { row.BundleSHA256 = strings.Repeat("c", 64) }, model.ErrProfileStale},
		{"different variant", func(row *model.EligibilityRow) { row.VariantID = "replacement" }, model.ErrProfileStale},
		{"platform drift", func(row *model.EligibilityRow) { row.PlatformID = "other" }, model.ErrProfileStale},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory, eligibility := controlSelectionFixture(t)
			test.change(&eligibility.rows["game"][0])
			room, err := service.SetReady(t.Context(), "room", "guest", true, 4)
			if !errors.Is(err, test.want) {
				t.Fatalf("ready error=%v want=%v", err, test.want)
			}
			if test.want != nil {
				if memory.writes != 0 || room.RoomID != "" {
					t.Fatalf("stale snapshot wrote room=%+v writes=%d", room, memory.writes)
				}
			} else if memory.writes != 1 || !memory.ready.Ready {
				t.Fatalf("ready plan=%+v writes=%d", memory.ready, memory.writes)
			}
		})
	}
}

func TestRoomSelectionUsesCurrentEligibilityAndRejectsOutOfRangeOccupants(t *testing.T) {
	t.Parallel()
	service, memory, eligibility := controlSelectionFixture(t)
	if _, err := service.SelectGame(t.Context(), "room", "host", "game", "fceumm-423-v1", 4); err != nil || memory.writes != 1 {
		t.Fatalf("selection error=%v writes=%d", err, memory.writes)
	}
	memory.writes = 0
	memory.before.Occupants = []model.SeatMember{{PlayerNo: 3}}
	if _, err := service.SelectGame(t.Context(), "room", "host", "game", "fceumm-423-v1", 4); !errors.Is(err, model.ErrInvalidSeat) || memory.writes != 0 {
		t.Fatalf("occupied seat error=%v writes=%d", err, memory.writes)
	}
	memory.before.Occupants = nil
	sentinel := errors.New("eligibility unavailable")
	eligibility.failure = sentinel
	if _, err := service.SetReady(t.Context(), "room", "guest", true, 4); !errors.Is(err, sentinel) || memory.writes != 0 {
		t.Fatalf("eligibility failure=%v writes=%d", err, memory.writes)
	}
}
