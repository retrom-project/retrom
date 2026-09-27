package launch

import (
	"cmp"
	"fmt"
	"slices"

	gamevariant "retrom/internal/service/gamevariant"

	corevalidation "retrom/internal/core/validation"
)

func approvedProductBIOS(snapshot ProductSnapshot) (ProductSnapshot, *corevalidation.Snapshot, error) {
	if snapshot.Source.CompatibilityCode != "REVIEW_SCREENSHOT_OVERRIDE" {
		return snapshot, nil, nil
	}
	current, _, _, err := gamevariant.ResolveBIOS(snapshot.Source, snapshot.BIOS, snapshot.Source.ContentLogicalName)
	if err != nil {
		return ProductSnapshot{}, nil, fmt.Errorf("resolve approved BIOS: %w", err)
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
	snapshot.VariantFiles = append([]gamevariant.File(nil), snapshot.VariantFiles...)
	for index, dependency := range current.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.FileRecord == nil {
			continue
		}
		snapshot.VariantFiles = refreshProductBIOSFile(snapshot.VariantFiles, dependency, index)
	}
	slices.SortFunc(snapshot.VariantFiles, compareProductVariantFiles)
	return snapshot, &current, nil
}

func refreshProductBIOSFile(
	files []gamevariant.File, dependency corevalidation.BIOSDependency, index int,
) []gamevariant.File {
	for position, file := range files {
		if file.Role == "BIOS_BUNDLE" && file.LogicalName == dependency.LogicalName {
			if file.FileRecord != *dependency.FileRecord {
				files[position].FileRecord = *dependency.FileRecord
				files[position].SortOrder = index
			}
			return files
		}
	}
	return append(
		files,
		gamevariant.File{
			Role: "BIOS_BUNDLE", LogicalName: dependency.LogicalName,
			FileRecord: *dependency.FileRecord, SortOrder: index,
		},
	)
}

func compareProductVariantFiles(left, right gamevariant.File) int {
	if order := cmp.Compare(left.Role, right.Role); order != 0 {
		return order
	}
	if order := cmp.Compare(left.SortOrder, right.SortOrder); order != 0 {
		return order
	}
	return cmp.Compare(left.LogicalName, right.LogicalName)
}
