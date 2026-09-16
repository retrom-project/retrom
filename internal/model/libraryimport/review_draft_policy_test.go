package libraryimport

import (
	"errors"
	"testing"
)

func TestValidateDraftAssetSelectionAcceptsValidInput(t *testing.T) {
	t.Parallel()
	cover := "cover-001"
	err := ValidateDraftAssetSelection(
		[]string{"screenshot-1", "screenshot-2"},
		&cover, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDraftAssetSelectionRejectsTooManyScreenshots(t *testing.T) {
	t.Parallel()
	ids := make([]string, 33)
	for i := range ids {
		ids[i] = "screenshot"
	}
	err := ValidateDraftAssetSelection(ids, nil, nil)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}
}

func TestValidateDraftAssetSelectionRejectsDuplicateScreenshots(t *testing.T) {
	t.Parallel()
	err := ValidateDraftAssetSelection(
		[]string{"same-id", "same-id"}, nil, nil,
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}
}

func TestValidateDraftAssetSelectionRejectsCoverAndUploadedCover(t *testing.T) {
	t.Parallel()
	cover, uploaded := "cover", "uploaded"
	err := ValidateDraftAssetSelection(nil, &cover, &uploaded)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}
}

func TestValidateRPGOverrideRejectsNonRPGWithOverride(t *testing.T) {
	t.Parallel()
	override := true
	err := ValidateRPGOverride(RPGOverrideFacts{
		IsRPG:                 false,
		SelfContainedOverride: &override,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}
}

func TestValidateRPGOverrideRejectsTargetChangeForRPG(t *testing.T) {
	t.Parallel()
	err := ValidateRPGOverride(RPGOverrideFacts{
		IsRPG:              true,
		TargetOrDOSChanged: true,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}
}

func TestValidateRPGOverrideRejectsMVMZSelfContained(t *testing.T) {
	t.Parallel()
	for _, gen := range []string{"RPGMV", "RPGMZ"} {
		override := true
		err := ValidateRPGOverride(RPGOverrideFacts{
			IsRPG:                 true,
			SelfContainedOverride: &override,
			Generation:            gen,
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("generation %s: expected ErrInvalid, got: %v", gen, err)
		}
	}
}

func TestValidateRPGOverrideAcceptsValidOverride(t *testing.T) {
	t.Parallel()
	override := true
	err := ValidateRPGOverride(RPGOverrideFacts{
		IsRPG:                 true,
		SelfContainedOverride: &override,
		Generation:            "RPG2K",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDraftTargetChangeRejectsCrossPlatform(t *testing.T) {
	t.Parallel()
	err := ValidateDraftTargetChange("platform-a", "platform-b")
	if !errors.Is(err, ErrReimportRequiredPlatformChange) {
		t.Fatalf("expected ErrReimportRequiredPlatformChange, got: %v", err)
	}
}

func TestValidateDraftTargetChangeAcceptsSamePlatform(t *testing.T) {
	t.Parallel()
	err := ValidateDraftTargetChange("platform-a", "platform-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDraftDOSEntryAcceptsKnownEntry(t *testing.T) {
	t.Parallel()
	allowed := map[string]struct{}{"DUKE3D": {}}
	err := ValidateDraftDOSEntry("DUKE3D", allowed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDraftDOSEntryRejectsUnknownEntry(t *testing.T) {
	t.Parallel()
	allowed := map[string]struct{}{"DUKE3D": {}}
	err := ValidateDraftDOSEntry("DOOM", allowed)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}
}
