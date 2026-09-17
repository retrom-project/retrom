package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/dependencies"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/runtime/runtimecatalog"

	"github.com/google/uuid"
)

var (
	ErrDATParseFailed = errors.New("DEPENDENCY_DAT_PARSE_FAILED")
	errBIOSOptions    = errors.New("DEPENDENCY_BIOS_ACTIVATION_OPTIONS_INVALID")
)

type Service struct {
	set        *dependencies.Set
	repository model.Repository
}

func New(set *dependencies.Set, repository model.Repository) *Service {
	return &Service{set: set, repository: repository}
}

func (service *Service) Bootstrap(ctx context.Context, now time.Time) error {
	preferred := preferredCoreVersions(service.set)
	cmd, err := service.buildBootstrapCommand(preferred, now)
	if err != nil {
		return fmt.Errorf("prepare bootstrap definitions: %w", err)
	}
	if err := service.repository.CommitBootstrapDefinitions(ctx, cmd); err != nil {
		return fmt.Errorf("bootstrap dependency definitions: %w", err)
	}
	return nil
}

func (service *Service) buildBootstrapCommand(
	preferred map[string]string, now time.Time,
) (model.BootstrapCommand, error) {
	cmd := model.BootstrapCommand{NowMS: now.UnixMilli()}
	for _, versionName := range service.set.Order {
		biosEntries, err := service.buildBIOSEntries(versionName, now)
		if err != nil {
			return model.BootstrapCommand{}, err
		}
		cmd.BIOSEntries = append(cmd.BIOSEntries, biosEntries...)
		datEntries, err := service.buildDATEntries(
			versionName, service.set.Versions[versionName], preferred, now,
		)
		if err != nil {
			return model.BootstrapCommand{}, err
		}
		cmd.DATEntries = append(cmd.DATEntries, datEntries...)
	}
	return cmd, nil
}

func (service *Service) buildBIOSEntries(
	versionName string, now time.Time,
) ([]model.BIOSRequirement, error) {
	catalog, err := completeStaticBIOSCatalog()
	if err != nil {
		return nil, err
	}
	if err := validateBIOSActivationOptions(catalog); err != nil {
		return nil, err
	}
	coreTargets := make(map[string]model.RuntimeTarget)
	coreResolved := make(map[string]bool)
	for _, req := range catalog {
		if coreResolved[req.coreID] {
			continue
		}
		coreResolved[req.coreID] = true
		target, err := targetForCore(service.set.RuntimeCatalog, req.coreID)
		if err != nil {
			continue
		}
		coreTargets[req.coreID] = target
	}
	var entries []model.BIOSRequirement
	for _, requirement := range catalog {
		target, ok := coreTargets[requirement.coreID]
		if !ok {
			continue
		}
		if requirement.providerID != "" &&
			(target.ProviderID != requirement.providerID || target.TargetID != requirement.targetID) {
			return nil, fmt.Errorf("%w: firmware target %s", errBIOSOptions, requirement.coreID)
		}
		entries = append(entries, buildSingleBIOSRequirement(requirement, target, versionName, now))
	}
	return entries, nil
}

func buildSingleBIOSRequirement(
	requirement staticBIOS, target model.RuntimeTarget, versionName string, now time.Time,
) model.BIOSRequirement {
	delivery := requirement.delivery
	if delivery == "" {
		delivery = "BIOS_BUNDLE"
	}
	canonical, _ := json.Marshal(
		map[string]any{
			"activationOptions": json.RawMessage(nullableJSON(requirement.options)),
			"conditionCode":     requirement.condition,
			"deliveryKind":      delivery,
			"emulatorPath":      nullableStringValue(requirement.emulatorPath),
			"logicalName":       requirement.logical,
			"archiveMembers":    json.RawMessage(nullableJSON(requirement.members)),
			"sourceDigest":      requirement.sourceDigest,
			"md5":               requirement.md5,
			"mode":              requirement.mode,
			"sha256":            nullableStringValue(requirement.sha256),
			"sizeBytes":         nullablePositive(requirement.size),
		},
	)
	digest := sha256.Sum256(canonical)
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(
		"retrom:bios:"+target.ProviderID+":"+target.TargetID+":"+requirement.logical,
	)).String()
	return model.BIOSRequirement{
		ID: id, CoreID: requirement.coreID,
		ProviderID: target.ProviderID, TargetID: target.TargetID,
		LogicalName: requirement.logical, Mode: requirement.mode,
		ConditionCode: requirement.condition,
		Options:       nullableOptions(requirement.options),
		Digest:        hex.EncodeToString(digest[:]),
		SizeBytes:     nullablePositive(requirement.size),
		MD5:           requirement.md5, SHA256: nullableStringValue(requirement.sha256),
		SourceURL:   requirement.sourceURL,
		VersionName: versionName, AtMS: now.UnixMilli(),
		Delivery:       delivery,
		EmulatorPath:   nullableStringValue(requirement.emulatorPath),
		ArchiveMembers: nullableStringValue(requirement.members),
	}
}

func (service *Service) buildDATEntries(
	versionName string, version *dependencies.Version,
	preferred map[string]string, now time.Time,
) ([]model.DATBootstrapEntry, error) {
	var entries []model.DATBootstrapEntry
	for _, core := range version.Manifest.Cores {
		if core.DAT == nil {
			continue
		}
		target, err := targetForCore(service.set.RuntimeCatalog, core.CoreID)
		if err != nil {
			return nil, err
		}
		entries = append(entries, model.DATBootstrapEntry{
			Registration: model.DATRegistration{
				CoreID: core.CoreID, Target: target,
				RelativePath: core.DAT.LocalPath, SHA256: core.DAT.SHA256,
				AtMS: now.UnixMilli(),
			},
			Expected: model.CatalogStats{
				MachineCount:              core.ParseStats.MachineCount,
				ROMEntryCount:             core.ParseStats.ROMEntryCount,
				DiskEntryCount:            core.ParseStats.DiskEntryCount,
				BIOSSetCount:              core.ParseStats.BIOSSetCount,
				DefaultBIOSSetCount:       core.ParseStats.DefaultBIOSSetCount,
				ExplicitBIOSMachineCount:  core.ParseStats.ExplicitBIOSMachineCount,
				BaseDependencyTargetCount: core.ParseStats.BaseDependencyTargetCount,
				UnresolvedCloneofCount:    core.ParseStats.UnresolvedCloneofCount + core.ParseStats.UnresolvedRomofCount,
			},
			Preferred: preferred[core.CoreID] == versionName,
		})
	}
	return entries, nil
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

func targetForCore(catalog runtimecatalog.Catalog, coreID string) (model.RuntimeTarget, error) {
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
			return model.RuntimeTarget{}, fmt.Errorf(
				"%w: ambiguous runtime target for core %s",
				dependencies.ErrInvalid,
				coreID,
			)
		}
	}
	if selected == nil {
		return model.RuntimeTarget{}, fmt.Errorf(
			"%w: runtime target missing for core %s",
			dependencies.ErrInvalid,
			coreID,
		)
	}
	return model.RuntimeTarget{ProviderID: selected.ProviderID, TargetID: selected.TargetID}, nil
}
