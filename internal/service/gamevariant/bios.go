package gamevariant

import (
	"encoding/json"
	"fmt"
	"slices"

	corevalidation "retrom/internal/core/validation"
	validation "retrom/internal/service/corevalidation"
)

func ResolveBIOS(
	source Source,
	facts BIOSFacts,
	logicalName string,
) (corevalidation.Snapshot, string, string, error) {
	if source.ProviderID == "retrom-runtime" && source.TargetID == "scummvm" {
		return corevalidation.Snapshot{
			SchemaVersion: 1,
			Kind:          corevalidation.SnapshotKindStatic,
			BIOS:          []corevalidation.BIOSDependency{},
		}, "READY", "READY", nil
	}
	if source.ProviderID == "" || source.TargetID == "" {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", corevalidation.ErrInvalidSnapshot
	}
	snapshot, status, code, err := validation.ResolveBIOSRecords(facts.Static, logicalName)
	if err != nil {
		return snapshot, status, code, fmt.Errorf("resolve product static BIOS: %w", err)
	}
	for _, record := range facts.Arcade {
		if record.State == "SATISFIED_BY_CONTENT" {
			continue
		}
		if !record.CatalogPresent {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
			continue
		}
		dependency := record.Dependency
		dependency.ActivationOptions = map[string]string{}
		if record.OptionsJSON != nil {
			if err := json.Unmarshal([]byte(*record.OptionsJSON), &dependency.ActivationOptions); err != nil {
				return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", corevalidation.ErrInvalidSnapshot
			}
		}
		if dependency.FileRecord == nil || dependency.InstallationStatus == nil || !corevalidation.BIOSInstallationUsable(
			*dependency.InstallationStatus,
		) {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		snapshot.BIOS = append(snapshot.BIOS, dependency)
	}
	return snapshot, status, code, nil
}

func BIOSFresh(snapshot Snapshot) (bool, error) {
	if snapshot.Source.CompatibilityCode == "REVIEW_SCREENSHOT_OVERRIDE" {
		return true, nil
	}
	current, _, _, err := ResolveBIOS(snapshot.Source, snapshot.BIOS, snapshot.Source.ContentLogicalName)
	if err != nil {
		return false, err
	}
	if snapshot.Source.DATVersionID != nil {
		return productArcadeBIOSFresh(current, snapshot.VariantFiles), nil
	}
	locked, err := corevalidation.ParseRuntimeBIOSDependencies(snapshot.Source.DependencySnapshot)
	if err != nil {
		if len(current.BIOS) == 0 {
			return true, nil
		}
		return false, fmt.Errorf("parse locked product BIOS: %w", err)
	}
	frozen := corevalidation.Snapshot{
		SchemaVersion: corevalidation.SnapshotSchemaVersion,
		Kind:          corevalidation.SnapshotKindStatic,
		BIOS:          locked,
	}
	currentDigest, err := corevalidation.BIOSDependencyDigest(current)
	if err != nil {
		return false, fmt.Errorf("digest current product BIOS: %w", err)
	}
	frozenDigest, err := corevalidation.BIOSDependencyDigest(frozen)
	if err != nil {
		return false, fmt.Errorf("digest frozen product BIOS: %w", err)
	}
	return currentDigest == frozenDigest, nil
}

func productArcadeBIOSFresh(current corevalidation.Snapshot, files []File) bool {
	type identity struct{ name, blob string }
	wanted := make([]identity, 0)
	locked := make([]identity, 0)
	for _, dependency := range current.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.FileRecord != nil {
			wanted = append(wanted, identity{dependency.LogicalName, *dependency.FileRecord})
		}
	}
	for _, file := range files {
		if file.Role == "BIOS_BUNDLE" {
			locked = append(locked, identity{file.LogicalName, file.FileRecord})
		}
	}
	return slices.Equal(wanted, locked)
}
