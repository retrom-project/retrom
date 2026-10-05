package dependencies

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/format/arcadedat"
)

// UsePairedDAT replaces the target's inferred DAT source with a verified
// Provider-owned artifact before any DAT is registered or activated.
func (set *Set) UsePairedDAT(ctx context.Context, providerID, targetID, pathValue, digest, sourceCommit string) error {
	coreID, err := set.pairedDATCore(providerID, targetID)
	if err != nil {
		return err
	}

	info, err := os.Stat(pathValue)
	if err != nil {
		return fmt.Errorf("read paired DAT: %w", err)
	}
	if err := checkFile(filepath.Dir(pathValue), filepath.Base(pathValue), info.Size(), digest); err != nil {
		return err
	}
	file, err := os.Open(pathValue)
	if err != nil {
		return fmt.Errorf("open paired DAT: %w", err)
	}
	stats, parseErr := arcadedat.Parse(ctx, file, coreID)
	closeErr := file.Close()
	if parseErr != nil {
		return fmt.Errorf("parse paired DAT: %w", parseErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close paired DAT: %w", closeErr)
	}
	paired := &Version{Manifest: Manifest{SchemaVersion: 8, Cores: []Core{{CoreID: coreID, DAT: &DATArtifact{}}}}}
	// A Provider declaration supersedes every host-owned version of this core.
	for _, version := range set.Versions {
		for index := range version.Manifest.Cores {
			if version.Manifest.Cores[index].CoreID == coreID {
				version.Manifest.Cores[index].DAT = nil
			}
		}
	}

	paired.Paired = true
	paired.DATRoot = filepath.Dir(pathValue)
	paired.ManifestSHA256 = digest
	core := &paired.Manifest.Cores[0]
	core.DAT.LocalPath = filepath.Base(pathValue)
	core.DAT.SizeBytes = info.Size()
	core.DAT.SHA256 = digest
	core.CoreSource.Commit = sourceCommit
	core.CoreSource.AssociationStatus = "paired_with_shipped_wasm"
	core.ParseStats.MachineCount = int64(stats.MachineCount)
	core.ParseStats.ROMEntryCount = int64(stats.ROMEntryCount)
	core.ParseStats.DiskEntryCount = int64(stats.DiskEntryCount)
	core.ParseStats.BIOSSetCount = int64(stats.BIOSSetCount)
	core.ParseStats.DefaultBIOSSetCount = int64(stats.DefaultBIOSSetCount)
	core.ParseStats.ExplicitBIOSMachineCount = int64(stats.ExplicitBIOSMachineCount)
	core.ParseStats.BaseDependencyTargetCount = int64(stats.BaseDependencyTargetCount)
	core.ParseStats.UnresolvedCloneofCount = int64(stats.UnresolvedCloneofTargetCount)
	core.ParseStats.UnresolvedRomofCount = int64(stats.UnresolvedRomofTargetCount)
	name := "provider/" + providerID + "/" + targetID + "/" + digest
	paired.Manifest.EmulatorJS.Version = name
	set.Versions[name] = paired
	set.Order = append(set.Order, name)
	return nil
}

func (set *Set) pairedDATCore(providerID, targetID string) (string, error) {
	coreID := ""
	for _, binding := range set.RuntimeCatalog.Bindings {
		if binding.ProviderID == providerID && binding.TargetID == targetID {
			if coreID != "" && coreID != binding.CoreID {
				return "", fmt.Errorf("%w: ambiguous paired DAT target", ErrInvalid)
			}
			coreID = binding.CoreID
		}
	}
	if coreID == "" || !arcadedat.SupportsCore(coreID) {
		return "", fmt.Errorf("%w: paired DAT target unsupported", ErrInvalid)
	}
	return coreID, nil
}
