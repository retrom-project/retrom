package arcade

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corevalidation "retrom/internal/core/validation"
	"retrom/internal/importing"
)

// Archive contains observations of imported bytes, independent of DAT and installed BIOS.
type Archive struct {
	LogicalName, FileRecord, Role string
	Entries                       map[string]importing.ArchiveEntry
}
type Result struct {
	Snapshot     Snapshot
	Status, Code string
	BIOS         []corevalidation.BIOSDependency
	Companions   []Resource
}
type evaluation struct {
	ctx              context.Context
	catalog          Catalog
	bios             BIOSReader
	provider, target string
	primary          Archive
	companions       map[string]Archive
	mergedROMSet     bool
	result           Result
}

func Resolve(ctx context.Context, catalog Catalog, bios BIOSReader,
	provider, target, datID, machine string, archives []Archive,
) (Result, error) {
	run := evaluation{
		ctx: ctx, catalog: catalog, bios: bios, provider: provider, target: target,
		companions: map[string]Archive{},
		result: Result{Status: "READY", Code: "READY", Snapshot: Snapshot{
			SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindArcade,
			Machine: machine, DatVersionID: datID,
			Dependencies: []Dependency{}, MissingEntries: []string{},
			MismatchedEntries: []string{}, Warnings: []string{},
		}},
	}
	for _, archive := range archives {
		if archive.Role == "CONTENT" {
			run.primary = archive
		} else {
			run.companions[archive.LogicalName] = archive
		}
	}
	nodes, cyclic, err := LoadClosure(ctx, catalog, datID, machine)
	if errors.Is(err, ErrInvalid) {
		run.result.Status, run.result.Code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		return run.result, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("resolve current arcade closure: %w", err)
	}
	if cyclic {
		run.result.Status, run.result.Code = "INCOMPATIBLE", "ARCADE_DEPENDENCY_CYCLE"
		return run.result, nil
	}
	run.result.Snapshot.Closure = nodes
	for _, node := range nodes {
		if err := run.check(node); err != nil {
			return Result{}, err
		}
	}
	if run.mergedROMSet && run.result.Status != "INCOMPATIBLE" {
		run.result.Status, run.result.Code = "BLOCKED", "UNSUPPORTED_MERGED_ROMSET"
	}
	sort.Strings(run.result.Snapshot.MissingEntries)
	sort.Strings(run.result.Snapshot.MismatchedEntries)
	sort.Strings(run.result.Snapshot.Warnings)
	sort.Slice(run.result.Snapshot.Dependencies, func(i, j int) bool {
		a, b := run.result.Snapshot.Dependencies[i], run.result.Snapshot.Dependencies[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return a.Machine < b.Machine
	})
	return run.result, nil
}

func (run *evaluation) check(node ClosureNode) error {
	facts, err := LoadRequirements(run.ctx, run.catalog, run.result.Snapshot.DatVersionID, node.Machine)
	if err != nil {
		return fmt.Errorf("read current arcade requirements: %w", err)
	}
	if facts.HasDisk {
		run.result.Status, run.result.Code = "INCOMPATIBLE", "UNSUPPORTED_CHD"
		return nil
	}
	requirements := facts.Owned
	all := append(append([]ROMRequirement(nil), requirements...), facts.Inherited...)
	run.mergedROMSet = run.mergedROMSet || containsMergedEntries(run.primary.Entries, all)
	if node.Kind == "CONTENT" {
		direct := facts.Archive(run.primary.Entries)
		missing, mismatch, warnings := MatchRequirements(run.primary.Entries, direct)
		run.result.Snapshot.MissingEntries = append(run.result.Snapshot.MissingEntries, missing...)
		run.result.Snapshot.MismatchedEntries = append(run.result.Snapshot.MismatchedEntries, mismatch...)
		run.result.Snapshot.Warnings = append(run.result.Snapshot.Warnings, warnings...)
		if len(missing)+len(mismatch) > 0 {
			run.result.Status, run.result.Code = "BLOCKED", "ARCADE_CONTENT_MISSING_ENTRY"
		}
		return nil
	}
	dependency := Dependency{
		Kind: node.Kind, Machine: node.Machine, RequiredBy: node.RequiredBy, Depth: node.Depth,
		ExpectedLogicalName: node.Machine + ".zip", RequiredEntries: requirementNames(requirements),
		RequiredEntryCount: len(requirements), State: "MISSING",
	}
	if err := run.resolveDependency(&dependency, facts); err != nil {
		return err
	}
	run.result.Snapshot.Dependencies = append(run.result.Snapshot.Dependencies, dependency)
	if dependency.State == "MISSING" {
		run.result.Snapshot.MissingEntries = append(run.result.Snapshot.MissingEntries, dependency.ExpectedLogicalName)
		if run.result.Status == "READY" {
			run.result.Status, run.result.Code = "BLOCKED", "LAUNCH_BIOS_MISSING"
			if node.Kind == "PARENT" {
				run.result.Code = "LAUNCH_PARENT_MISSING"
			}
		}
	}
	return nil
}

func (run *evaluation) resolveDependency(
	dependency *Dependency, requirements Requirements,
) error {
	missing, mismatch, _ := MatchRequirements(run.primary.Entries, requirements.Archive(run.primary.Entries))
	if len(missing)+len(mismatch) == 0 {
		dependency.State = "SATISFIED_BY_CONTENT"
		return nil
	}
	if archive, found := run.companions[dependency.ExpectedLogicalName]; found {
		return run.resolveCompanion(dependency, requirements, archive)
	}

	if dependency.Kind != "BIOS_OR_BASE" {
		return nil
	}
	resolved, found, err := run.bios.BIOS(run.ctx, run.provider, run.target, dependency.ExpectedLogicalName)
	if err != nil {
		return fmt.Errorf("read current arcade BIOS: %w", err)
	}
	if !found {
		return nil
	}
	dependency.State = "SATISFIED_EXTERNAL"
	if resolved.InstallationStatus != nil && *resolved.InstallationStatus != "MATCHED" {
		dependency.State = "HASH_WARNING"
		run.result.Snapshot.Warnings = append(run.result.Snapshot.Warnings, dependency.ExpectedLogicalName)
	}
	run.result.BIOS = append(run.result.BIOS, resolved)
	return nil
}

func (run *evaluation) resolveCompanion(
	dependency *Dependency, requirements Requirements, archive Archive,
) error {
	missing, mismatch, warnings := MatchRequirements(archive.Entries, requirements.Archive(archive.Entries))
	dependency.State = "SATISFIED_EXTERNAL"
	if len(missing)+len(mismatch) > 0 {
		if dependency.Kind == "PARENT" {
			dependency.State = "MISMATCH"
			if run.result.Status != "INCOMPATIBLE" {
				run.result.Status, run.result.Code = "BLOCKED", "ARCADE_DEPENDENCY_MISMATCH"
			}
			run.result.Snapshot.MissingEntries = append(run.result.Snapshot.MissingEntries, missing...)
			run.result.Snapshot.MismatchedEntries = append(run.result.Snapshot.MismatchedEntries, mismatch...)
			return nil
		}
		dependency.State = "HASH_WARNING"
		warnings = append(warnings, missing...)
		warnings = append(warnings, mismatch...)
	}
	run.result.Snapshot.Warnings = append(run.result.Snapshot.Warnings, warnings...)
	role := "PARENT"
	if dependency.Kind == "BIOS_OR_BASE" {
		role = "BIOS_BUNDLE"
	}
	run.result.Companions = append(run.result.Companions, Resource{
		Role: role, LogicalName: archive.LogicalName, FileRecord: archive.FileRecord, SortOrder: len(run.result.Companions),
	})
	return nil
}
