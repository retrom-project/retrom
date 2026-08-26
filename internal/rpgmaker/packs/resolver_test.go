package packs

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/rpgmaker/detector"
	"retrom/internal/testassert"
)

func TestResolveDefaultsUniqueRGSSInstallationAndFreezesSlots(t *testing.T) {
	definitionID := uuid.NewString()
	installationID := uuid.NewString()
	resolution, err := Resolve(
		detector.RPGXP, false, false,
		[]Requirement{{Slot: 2, DeclaredName: " Standard "}},
		[]Definition{{
			ID: definitionID, Generation: detector.RPGXP, DeclaredName: "Standard",
			NormalizedDeclaredName: "standard", Enabled: true,
		}},
		[]Installation{{
			ID: installationID, DefinitionID: definitionID, FilesDigest: repeat("a", 64), Status: "READY",
		}}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return len(resolution.Bindings) != 1 },
		func() bool { return resolution.Bindings[0].Slot != 2 },
		func() bool { return resolution.Bindings[0].InstallationID != installationID },
		func() bool { return len(resolution.DependencySHA256) != 64 },
	), "resolution=%#v", resolution)
}

func TestResolveRequiresExplicitSelectionWhenMultipleVersionsReady(t *testing.T) {
	definitionID := uuid.NewString()
	first, second := uuid.NewString(), uuid.NewString()
	definitions := []Definition{{
		ID: definitionID, Generation: detector.RPGVX, DeclaredName: "RPGVX",
		NormalizedDeclaredName: "rpgvx", Enabled: true,
	}}
	installations := []Installation{
		{ID: first, DefinitionID: definitionID, FilesDigest: repeat("a", 64), Status: "READY"},
		{ID: second, DefinitionID: definitionID, FilesDigest: repeat("b", 64), Status: "READY"},
	}
	requirements := []Requirement{{Slot: 1, DeclaredName: "RPGVX"}}
	if _, err := Resolve(detector.RPGVX, false, false, requirements, definitions, installations, nil); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("error=%v, want %v", err, ErrAmbiguous)
	}
	resolution, err := Resolve(
		detector.RPGVX, false, false, requirements, definitions, installations,
		[]Selection{{Slot: 1, InstallationID: second}},
	)
	if err != nil || resolution.Bindings[0].InstallationID != second {
		t.Fatalf("resolution=%#v error=%v", resolution, err)
	}
}

func TestResolveEasyRPGSelfContainedAndRequiredRTP(t *testing.T) {
	resolution, err := Resolve(detector.RPG2000, true, false, nil, nil, nil, nil)
	if err != nil || len(resolution.Bindings) != 0 {
		t.Fatalf("self-contained=%#v error=%v", resolution, err)
	}
	if _, err := Resolve(detector.RPG2000, false, false, nil, nil, nil, nil); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing RTP error=%v", err)
	}
}

func TestResolveRejectsWrongSelectionAndNativePacks(t *testing.T) {
	selection := Selection{Slot: 1, InstallationID: uuid.NewString()}
	if _, err := Resolve(detector.RPGMV, false, false, nil, nil, nil, []Selection{selection}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("native selection error=%v", err)
	}
	if _, err := Resolve(
		detector.RPGVXAce, false, false,
		[]Requirement{{Slot: 1, DeclaredName: "RPGVXAce"}}, nil, nil, []Selection{selection},
	); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing definition error=%v", err)
	}
}

func TestNormalizeDeclaredNameUsesNFKCCaseFold(t *testing.T) {
	if got := NormalizeDeclaredName(" ＲＰＧＶＸ "); got != "rpgvx" {
		t.Fatalf("normalized=%q", got)
	}
}

func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
