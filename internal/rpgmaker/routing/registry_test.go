package routing

import (
	"errors"
	"testing"

	"retrom/internal/rpgmaker/detector"
	"retrom/internal/testassert"
)

func TestRegistryHasOneCurrentRouteForEveryVisibleCore(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	if len(Entries()) != 7 {
		t.Fatalf("entries = %d, want 7", len(Entries()))
	}
	for coreID := range supportedCoreIDs() {
		generation, err := detector.GenerationForCore(coreID)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := Current(coreID, generation)
		testassert.Falsef(t, testassert.Any(
			func() bool { return err != nil },
			func() bool { return entry.CoreID != coreID },
			func() bool { return entry.Generation != generation },
		), "Current(%s,%s)=%#v error=%v", coreID, generation, entry, err)
	}
}

func TestCleanRegistryDoesNotExposeMigrationRoutes(t *testing.T) {
	for _, test := range []struct{ coreID, route string }{
		{coreID: "rpgmaker_2000", route: "RPG2000_UNDECLARED"},
		{coreID: "rpgmaker_xp", route: "RPGXP_UNDECLARED"},
		{coreID: "rpgmaker_mv", route: "RPGMV_UNDECLARED"},
		{coreID: "rpgmaker_mz", route: "RPGMZ_UNDECLARED"},
	} {
		if _, err := ByRoute(test.coreID, test.route); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("ByRoute(%s,%s) error=%v", test.coreID, test.route, err)
		}
	}
}

func TestRepositoryTagIsTheOnlyRuntimeVersion(t *testing.T) {
	current, err := Current("rpgmaker_2000", detector.RPG2000)
	if err != nil || current.RouteKey != "RPG2000_EASYRPG" || current.RuntimeVersion != "v0.7.3" {
		t.Fatalf("current route=%#v error=%v", current, err)
	}
	for _, entry := range Entries() {
		if entry.RuntimeVersion != current.RuntimeVersion || !entry.SelectedForNewBindings {
			t.Fatalf("route carries a parallel version: %#v", entry)
		}
	}
}

func TestCurrentNeverFallsBackAcrossGenerationOrCore(t *testing.T) {
	for _, input := range []struct {
		coreID     string
		generation detector.Generation
	}{
		{coreID: "rpgmaker_2000", generation: detector.RPG2003},
		{coreID: "rpgmaker_mz", generation: detector.RPGMV},
		{coreID: "unknown", generation: detector.RPGMV},
	} {
		if _, err := Current(input.coreID, input.generation); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Current(%s,%s) error=%v, want %v", input.coreID, input.generation, err, ErrUnavailable)
		}
	}
}

func TestRouteSpecificTechnicalRequirements(t *testing.T) {
	tests := map[string]struct {
		adapter      AdapterKind
		threads      bool
		payloadKind  string
		maxSaveBytes int64
	}{
		"rpgmaker_2000": {adapter: AdapterEasyRPG, payloadKind: PayloadNativeBundle, maxSaveBytes: 64 << 20},
		"rpgmaker_xp":   {adapter: AdapterMkxp, threads: true, payloadKind: PayloadRuntimeState, maxSaveBytes: 256 << 20},
		"rpgmaker_mz":   {adapter: AdapterNativeWeb, payloadKind: PayloadNativeBundle, maxSaveBytes: 64 << 20},
	}
	for coreID, want := range tests {
		generation, _ := detector.GenerationForCore(coreID)
		entry, err := Current(coreID, generation)
		if err != nil {
			t.Fatal(err)
		}
		testassert.Falsef(t, testassert.Any(
			func() bool { return entry.AdapterKind != want.adapter },
			func() bool { return entry.RequiresThreads != want.threads },
			func() bool { return entry.SavePayloadKind != want.payloadKind },
			func() bool { return entry.SaveMaxBytes != want.maxSaveBytes },
		), "route=%#v want=%#v", entry, want)
	}
}

func TestByRouteFindsCurrentShapeWithoutCrossCoreFallback(t *testing.T) {
	entry, err := ByRoute("rpgmaker_vx_ace", "RPGVXACE_MKXP")
	if err != nil || entry.RGSSVersion != 3 {
		t.Fatalf("route=%#v error=%v", entry, err)
	}
	if _, err := ByRoute("rpgmaker_vx", "RPGVXACE_MKXP"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cross-core route error=%v", err)
	}
}
