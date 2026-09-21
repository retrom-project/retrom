package dependencies

import (
	"context"
	"errors"
	"fmt"
	"time"

	"retrom/internal/dependencies"
	"retrom/internal/runtimecatalog"
)

var (
	ErrDATJobNotClaimed = errors.New("DEPENDENCY_DAT_JOB_NOT_CLAIMABLE")
	ErrDATParseFailed   = errors.New("DEPENDENCY_DAT_PARSE_FAILED")
	errBIOSOptions      = errors.New("DEPENDENCY_BIOS_ACTIVATION_OPTIONS_INVALID")
)

type Service struct {
	set        *dependencies.Set
	repository Repository
}

func New(set *dependencies.Set, repository Repository) *Service {
	return &Service{set: set, repository: repository}
}

func (service *Service) Bootstrap(ctx context.Context, now time.Time) error {
	preferred := preferredCoreVersions(service.set)
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		for _, versionName := range service.set.Order {
			targets, err := service.staticBIOSTargets(ctx, scope.Targets)
			if err != nil {
				return err
			}
			if err := bootstrapStaticBIOS(ctx, scope.BIOS, versionName, targets, now); err != nil {
				return err
			}
			if err := service.bootstrapVersionDATs(
				ctx,
				scope,
				versionName,
				service.set.Versions[versionName],
				targets,
				preferred,
				now,
			); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("bootstrap dependency definitions: %w", err)
	}
	return nil
}

func preferredCoreVersions(set *dependencies.Set) map[string]string {
	result := make(map[string]string)
	for _, versionName := range set.Order {
		for _, core := range set.Versions[versionName].Manifest.Cores {
			result[core.CoreID] = versionName
		}
	}
	return result
}

func targetForCore(catalog runtimecatalog.Catalog, coreID string) (RuntimeTarget, error) {
	var selected *runtimecatalog.Binding
	for index := range catalog.Bindings {
		binding := &catalog.Bindings[index]
		if binding.CoreID != coreID {
			continue
		}
		if selected == nil {
			selected = binding
			continue
		}
		if selected.ProviderID != binding.ProviderID || selected.TargetID != binding.TargetID {
			return RuntimeTarget{}, fmt.Errorf("%w: ambiguous runtime target for core %s", dependencies.ErrInvalid, coreID)
		}
	}
	if selected == nil {
		return RuntimeTarget{}, fmt.Errorf("%w: runtime target missing for core %s", dependencies.ErrInvalid, coreID)
	}
	return RuntimeTarget{ProviderID: selected.ProviderID, TargetID: selected.TargetID}, nil
}

func (service *Service) staticBIOSTargets(
	ctx context.Context,
	records TargetRecords,
) (map[string]RuntimeTarget, error) {
	catalog, err := completeStaticBIOSCatalog()
	if err != nil {
		return nil, err
	}
	result := make(map[string]RuntimeTarget, len(catalog))
	for _, requirement := range catalog {
		if _, exists := result[requirement.coreID]; exists {
			continue
		}
		target, err := service.seedTarget(ctx, records, requirement.coreID)
		if err != nil {
			return nil, err
		}
		result[requirement.coreID] = target
	}
	return result, nil
}

func (service *Service) seedTarget(ctx context.Context, records TargetRecords, coreID string) (RuntimeTarget, error) {
	target, err := targetForCore(service.set.RuntimeCatalog, coreID)
	if err != nil {
		return RuntimeTarget{}, err
	}
	exists, err := records.Exists(ctx, target)
	if err != nil {
		return RuntimeTarget{}, fmt.Errorf("read runtime target: %w", err)
	}
	if !exists {
		return RuntimeTarget{}, fmt.Errorf("%w: runtime target missing for core %s", dependencies.ErrInvalid, coreID)
	}
	return target, nil
}

func (service *Service) bootstrapVersionDATs(
	ctx context.Context, scope WriteScope, versionName string, version *dependencies.Version,
	targets map[string]RuntimeTarget, preferred map[string]string, now time.Time,
) error {
	for _, core := range version.Manifest.Cores {
		if core.DAT == nil {
			continue
		}
		target, exists := targets[core.CoreID]
		if !exists {
			var err error
			target, err = service.seedTarget(ctx, scope.Targets, core.CoreID)
			if err != nil {
				return err
			}
			targets[core.CoreID] = target
		}
		expected := CatalogStats{
			MachineCount: core.ParseStats.MachineCount, ROMEntryCount: core.ParseStats.ROMEntryCount,
			DiskEntryCount: core.ParseStats.DiskEntryCount, BIOSSetCount: core.ParseStats.BIOSSetCount,
			DefaultBIOSSetCount:       core.ParseStats.DefaultBIOSSetCount,
			ExplicitBIOSMachineCount:  core.ParseStats.ExplicitBIOSMachineCount,
			BaseDependencyTargetCount: core.ParseStats.BaseDependencyTargetCount,
			UnresolvedCloneofCount:    core.ParseStats.UnresolvedCloneofCount + core.ParseStats.UnresolvedRomofCount,
		}
		registered, err := scope.DAT.Register(ctx, DATRegistration{
			CoreID: core.CoreID, Target: target,
			RelativePath: core.DAT.LocalPath, SHA256: core.DAT.SHA256, AtMS: now.UnixMilli(),
		})
		if err != nil {
			return fmt.Errorf("register built-in DAT: %w", err)
		}
		if registered.ParseStatus == "READY" && registered.Stats != expected {
			if err := scope.DAT.Reset(ctx, registered.ID, now.UnixMilli()); err != nil {
				return fmt.Errorf("reset incomplete DAT index: %w", err)
			}
		}
		if preferred[core.CoreID] == versionName {
			if err := scope.DAT.Retire(ctx, target, registered.ID, now.UnixMilli()); err != nil {
				return fmt.Errorf("retire superseded DAT: %w", err)
			}
		}
	}
	return nil
}
