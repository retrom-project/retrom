package libraryimport

import (
	"context"
	"fmt"
	"slices"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

func ReadImportTarget(ctx context.Context, reader model.ImportFactsReader, id string) (model.ImportTarget, error) {
	target, found, err := reader.Target(ctx, id)
	if err != nil {
		return model.ImportTarget{}, fmt.Errorf("read import target: %w", err)
	}
	if !found {
		return model.ImportTarget{}, model.ErrInvalid
	}
	target.CoreID = target.DefaultCoreID
	if target.PlatformID == "rpgmaker" && target.DefaultCoreID == detector.VirtualCoreID {
		return target, nil
	}
	return ResolveImportBinding(ctx, reader, target, "")
}

func ResolveImportBinding(
	ctx context.Context, reader model.ImportFactsReader, target model.ImportTarget, generation string,
) (model.ImportTarget, error) {
	bindings, err := reader.Bindings(ctx, model.ImportBindingQuery{
		PlatformID: target.PlatformID, CoreID: target.CoreID, DetectorProfile: generation,
	})
	if err != nil {
		return model.ImportTarget{}, fmt.Errorf("read import runtime bindings: %w", err)
	}
	if len(bindings) == 0 || len(bindings[0].Policy.SupportedContentKinds) == 0 {
		return model.ImportTarget{}, model.ErrInvalid
	}
	return bindImportTarget(target, bindings[0]), nil
}

func bindImportTarget(target model.ImportTarget, binding model.ImportBinding) model.ImportTarget {
	target.BindingID, target.CoreID = binding.BindingID, binding.CoreID
	target.ProviderID, target.TargetID = binding.ProviderID, binding.TargetID
	target.DeliveryProfile, target.Policy = binding.DeliveryProfile, binding.Policy
	return target
}

func SnapshotImportTarget(
	ctx context.Context, reader model.ImportFactsReader, target model.ImportTarget,
) (model.ImportTargetSnapshot, model.ImportTarget, error) {
	snapshot := model.ImportTargetSnapshot{
		SchemaVersion: 1, DefaultCoreID: target.DefaultCoreID, PlatformID: target.PlatformID,
		PlatformInstanceID: target.ID, PlatformInstanceVersion: target.Version,
	}
	if target.ProviderID != "" {
		snapshot.Targets = []model.ImportTargetGuard{TargetImportGuard(target)}
		return snapshot, target, nil
	}
	if target.PlatformID != "rpgmaker" || target.DefaultCoreID != detector.VirtualCoreID {
		return model.ImportTargetSnapshot{}, model.ImportTarget{}, model.ErrInvalid
	}
	bindings, err := reader.Bindings(ctx, model.ImportBindingQuery{PlatformID: "rpgmaker", CoreID: detector.VirtualCoreID})
	if err != nil {
		return model.ImportTargetSnapshot{}, model.ImportTarget{}, fmt.Errorf("read virtual import targets: %w", err)
	}
	if len(bindings) != 7 {
		return model.ImportTargetSnapshot{}, model.ImportTarget{}, model.ErrInvalid
	}
	for _, binding := range bindings {
		snapshot.Targets = append(snapshot.Targets, model.ImportTargetGuard{
			ProviderID: binding.ProviderID, TargetID: binding.TargetID, CoreID: binding.CoreID,
		})
	}
	slices.SortFunc(snapshot.Targets, func(left, right model.ImportTargetGuard) int {
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

func TargetImportGuard(target model.ImportTarget) model.ImportTargetGuard {
	return model.ImportTargetGuard{ProviderID: target.ProviderID, TargetID: target.TargetID, CoreID: target.CoreID}
}
