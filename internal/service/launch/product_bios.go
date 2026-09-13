package launch

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"retrom/internal/capability/content/corevalidation"
	validation "retrom/internal/service/corevalidation"
)

func ResolveProductBIOS(
	source ProductSource,
	facts ProductBIOSFacts,
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
		if dependency.BlobID == nil || dependency.InstallationStatus == nil || !corevalidation.BIOSInstallationUsable(
			*dependency.InstallationStatus,
		) {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		snapshot.BIOS = append(snapshot.BIOS, dependency)
	}
	return snapshot, status, code, nil
}

func productBIOSFresh(snapshot ProductSnapshot) (bool, error) {
	if snapshot.Source.CompatibilityCode == "REVIEW_SCREENSHOT_OVERRIDE" {
		return true, nil
	}
	current, _, _, err := ResolveProductBIOS(snapshot.Source, snapshot.BIOS, snapshot.Source.ContentLogicalName)
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

func productArcadeBIOSFresh(current corevalidation.Snapshot, files []ProductFile) bool {
	type identity struct{ name, blob string }
	wanted := make([]identity, 0)
	locked := make([]identity, 0)
	for _, dependency := range current.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			wanted = append(wanted, identity{dependency.LogicalName, *dependency.BlobID})
		}
	}
	for _, file := range files {
		if file.Role == "BIOS_BUNDLE" {
			locked = append(locked, identity{file.LogicalName, file.BlobID})
		}
	}
	return slices.Equal(wanted, locked)
}

func approvedProductBIOS(snapshot ProductSnapshot) (ProductSnapshot, *corevalidation.Snapshot, error) {
	if snapshot.Source.CompatibilityCode != "REVIEW_SCREENSHOT_OVERRIDE" {
		return snapshot, nil, nil
	}
	current, _, _, err := ResolveProductBIOS(snapshot.Source, snapshot.BIOS, snapshot.Source.ContentLogicalName)
	if err != nil {
		return ProductSnapshot{}, nil, err
	}
	if len(current.BIOS) == 0 {
		return snapshot, nil, nil
	}
	if snapshot.Source.DATVersionID == nil {
		locked, err := corevalidation.ParseSnapshot(snapshot.Source.DependencySnapshot)
		if err != nil {
			return ProductSnapshot{}, nil, fmt.Errorf("parse approved product BIOS: %w", err)
		}
		locked.BIOS = current.BIOS
		encoded, err := locked.JSON()
		if err != nil {
			return ProductSnapshot{}, nil, fmt.Errorf("encode approved product BIOS: %w", err)
		}
		snapshot.Source.DependencySnapshot = string(encoded)
	}
	snapshot.VariantFiles = append([]ProductFile(nil), snapshot.VariantFiles...)
	for index, dependency := range current.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		snapshot.VariantFiles = refreshProductBIOSFile(snapshot.VariantFiles, dependency, index)
	}
	slices.SortFunc(snapshot.VariantFiles, compareProductVariantFiles)
	return snapshot, &current, nil
}

func refreshProductBIOSFile(files []ProductFile, dependency corevalidation.BIOSDependency, index int) []ProductFile {
	for position, file := range files {
		if file.Role == "BIOS_BUNDLE" && file.LogicalName == dependency.LogicalName {
			if file.BlobID != *dependency.BlobID {
				files[position].BlobID = *dependency.BlobID
				files[position].SortOrder = index
			}
			return files
		}
	}
	return append(
		files,
		ProductFile{Role: "BIOS_BUNDLE", LogicalName: dependency.LogicalName, BlobID: *dependency.BlobID, SortOrder: index},
	)
}

func compareProductVariantFiles(left, right ProductFile) int {
	if order := cmp.Compare(left.Role, right.Role); order != 0 {
		return order
	}
	if order := cmp.Compare(left.SortOrder, right.SortOrder); order != 0 {
		return order
	}
	return cmp.Compare(left.LogicalName, right.LogicalName)
}
