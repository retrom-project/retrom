package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

func validateApprovalArcade(ctx context.Context, scope ApprovalDependencyScope, validationID, raw string) error {
	frozen, valid := ParseArcadeDraftSnapshot(raw)
	if !valid || len(frozen.MissingEntries) != 0 || len(frozen.MismatchedEntries) != 0 {
		return ErrInvalid
	}
	canonical, err := CanonicalArcadeSnapshot(ctx, scope.Arcade, raw)
	if err != nil {
		return err
	}
	var closure []ArcadeClosureNode
	if err := json.Unmarshal(canonical.Closure, &closure); err != nil {
		return fmt.Errorf("decode approval arcade closure: %w", err)
	}
	dependencyCount := 0
	for _, node := range closure {
		if node.Kind != "CONTENT" {
			dependencyCount++
		}
	}
	if len(canonical.Dependencies) != dependencyCount {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(canonical.Dependencies))
	for _, dependency := range canonical.Dependencies {
		key := dependency.Kind + "\x00" + dependency.Machine
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if err := validateApprovalArcadeDependency(ctx, scope.Reader, validationID,
			canonical.DatVersionID, dependency); err != nil {
			return err
		}
	}
	before, err := json.Marshal(frozen)
	if err != nil {
		return fmt.Errorf("encode frozen approval arcade snapshot: %w", err)
	}
	after, err := json.Marshal(canonical)
	if err != nil {
		return fmt.Errorf("encode current approval arcade snapshot: %w", err)
	}
	if !bytes.Equal(before, after) {
		return ErrInvalid
	}
	return nil
}

func validateApprovalArcadeDependency(
	ctx context.Context, reader ApprovalDependencyReader, validationID, datID string,
	dependency ArcadeDraftDependency,
) error {
	if dependency.State != "SATISFIED_BY_CONTENT" && dependency.State != "SATISFIED_EXTERNAL" &&
		dependency.State != "HASH_WARNING" {
		return ErrInvalid
	}
	requirements, err := reader.ArcadeRequirements(ctx, datID, dependency.Machine)
	if err != nil {
		return fmt.Errorf("read approved arcade requirements: %w", err)
	}
	if requirements.HasDisk || !sameApprovalRequirementNames(requirements, dependency.RequiredEntries) {
		return ErrInvalid
	}
	if dependency.State == "SATISFIED_BY_CONTENT" {
		return nil
	}
	role := ""
	switch dependency.Kind {
	case "PARENT":
		role = "PARENT"
	case "BIOS_OR_BASE":
		role = "BIOS_BUNDLE"
	}
	if role == "" {
		return ErrInvalid
	}
	count, err := reader.ExternalFileCount(ctx, validationID, role, dependency.ExpectedLogicalName)
	if err != nil {
		return fmt.Errorf("read approved arcade external files: %w", err)
	}
	if count != 1 {
		return ErrInvalid
	}
	return nil
}

func sameApprovalRequirementNames(requirements ApprovalArcadeRequirements, frozen []string) bool {
	index := 0
	for _, rom := range requirements.ROMs {
		if rom.Status == "NODUMP" || (rom.BIOSName != nil &&
			(requirements.DefaultBIOS == nil || *rom.BIOSName != *requirements.DefaultBIOS)) {
			continue
		}
		if index >= len(frozen) || rom.Name != frozen[index] {
			return false
		}
		index++
	}
	return index == len(frozen)
}
