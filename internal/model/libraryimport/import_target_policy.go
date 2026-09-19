package libraryimport

import (
	"context"
	"fmt"
	"slices"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

// ReadImportTarget reads and resolves the target for an import.
func ReadImportTarget(ctx context.Context, reader ImportFactsReader, id string) (ImportTarget, error) {
	target, found, err := reader.Target(ctx, id)
	if err != nil {
		return ImportTarget{}, fmt.Errorf("read import target: %w", err)
	}
	if !found {
		return ImportTarget{}, ErrInvalid
	}
	target.CoreID = target.DefaultCoreID
	if target.PlatformID == "rpgmaker" && target.DefaultCoreID == detector.VirtualCoreID {
		return target, nil
	}
	return ResolveImportBinding(ctx, reader, target, "")
}

// ResolveImportBinding resolves the runtime binding for an import target.
func ResolveImportBinding(
	ctx context.Context, reader ImportFactsReader, target ImportTarget, generation string,
) (ImportTarget, error) {
	bindings, err := reader.Bindings(ctx, ImportBindingQuery{
		PlatformID: target.PlatformID, CoreID: target.CoreID, DetectorProfile: generation,
	})
	if err != nil {
		return ImportTarget{}, fmt.Errorf("read import runtime bindings: %w", err)
	}
	if len(bindings) == 0 || len(bindings[0].Policy.SupportedContentKinds) == 0 {
		return ImportTarget{}, ErrInvalid
	}
	return BindImportTarget(target, bindings[0]), nil
}

// BindImportTarget applies a resolved binding to a target.
func BindImportTarget(target ImportTarget, binding ImportBinding) ImportTarget {
	target.BindingID, target.CoreID = binding.BindingID, binding.CoreID
	target.ProviderID, target.TargetID = binding.ProviderID, binding.TargetID
	target.DeliveryProfile, target.Policy = binding.DeliveryProfile, binding.Policy
	return target
}

// SnapshotImportTarget creates a frozen target snapshot for an import.
func SnapshotImportTarget(
	ctx context.Context, reader ImportFactsReader, target ImportTarget,
) (ImportTargetSnapshot, ImportTarget, error) {
	snapshot := ImportTargetSnapshot{
		SchemaVersion: 1, DefaultCoreID: target.DefaultCoreID, PlatformID: target.PlatformID,
		PlatformInstanceID: target.ID, PlatformInstanceVersion: target.Version,
	}
	if target.ProviderID != "" {
		snapshot.Targets = []ImportTargetGuard{TargetImportGuard(target)}
		return snapshot, target, nil
	}
	if target.PlatformID != "rpgmaker" || target.DefaultCoreID != detector.VirtualCoreID {
		return ImportTargetSnapshot{}, ImportTarget{}, ErrInvalid
	}
	bindings, err := reader.Bindings(ctx, ImportBindingQuery{PlatformID: "rpgmaker", CoreID: detector.VirtualCoreID})
	if err != nil {
		return ImportTargetSnapshot{}, ImportTarget{}, fmt.Errorf("read virtual import targets: %w", err)
	}
	if len(bindings) != 7 {
		return ImportTargetSnapshot{}, ImportTarget{}, ErrInvalid
	}
	for _, binding := range bindings {
		snapshot.Targets = append(snapshot.Targets, ImportTargetGuard{
			ProviderID: binding.ProviderID, TargetID: binding.TargetID, CoreID: binding.CoreID,
		})
	}
	slices.SortFunc(snapshot.Targets, func(left, right ImportTargetGuard) int {
		if left.ProviderID < right.ProviderID {
			return -1
		}
		if left.ProviderID > right.ProviderID {
			return 1
		}
		if left.TargetID < right.TargetID {
			return -1
		}
		if left.TargetID > right.TargetID {
			return 1
		}
		return 0
	})
	provisional := target
	first := snapshot.Targets[0]
	provisional.CoreID, provisional.ProviderID, provisional.TargetID = first.CoreID, first.ProviderID, first.TargetID
	return snapshot, provisional, nil
}

// TargetImportGuard creates a guard from a resolved target.
func TargetImportGuard(target ImportTarget) ImportTargetGuard {
	return ImportTargetGuard{ProviderID: target.ProviderID, TargetID: target.TargetID, CoreID: target.CoreID}
}
