package libraryimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"retrom/internal/content/arcade"
)

func validateApprovalArcade(ctx context.Context, scope ApprovalDependencyScope, itemID, raw string) error {
	frozen, valid := arcade.ParseSnapshot(raw)
	if !valid || len(frozen.MissingEntries) != 0 || len(frozen.MismatchedEntries) != 0 {
		return fmt.Errorf("invalid current arcade dependencies: %w", ErrInvalid)
	}
	canonical, err := arcade.CanonicalSnapshot(ctx, scope.Arcade, raw)
	if err != nil {
		if errors.Is(err, arcade.ErrInvalid) {
			return errors.Join(ErrInvalid, err)
		}
		return fmt.Errorf("resolve approval arcade snapshot: %w", err)
	}
	closure := canonical.Closure
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
		if err := validateApprovalArcadeDependency(ctx, scope, itemID,
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
		return fmt.Errorf("arcade snapshot does not match current DAT: %w", ErrInvalid)
	}
	return nil
}

func validateApprovalArcadeDependency(
	ctx context.Context, scope ApprovalDependencyScope, itemID, datID string,
	dependency arcade.Dependency,
) error {
	if dependency.State != "SATISFIED_BY_CONTENT" && dependency.State != "SATISFIED_EXTERNAL" &&
		dependency.State != "HASH_WARNING" {
		return ErrInvalid
	}
	requirements, err := arcade.LoadRequirements(ctx, scope.Arcade, datID, dependency.Machine)
	if err != nil {
		return fmt.Errorf("read approved arcade requirements: %w", err)
	}
	if requirements.HasDisk || !sameApprovalRequirementNames(requirements.Owned, dependency.RequiredEntries) {
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
	count, err := scope.Reader.ExternalFileCount(ctx, itemID, role, dependency.ExpectedLogicalName)
	if err != nil {
		return fmt.Errorf("read approved arcade external files: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("arcade external file %s count=%d: %w", dependency.ExpectedLogicalName, count, ErrInvalid)
	}
	return nil
}

func sameApprovalRequirementNames(requirements []arcade.ROMRequirement, frozen []string) bool {
	names := make([]string, 0, len(requirements))
	for _, rom := range requirements {
		names = append(names, rom.Name)
	}
	return slices.Equal(names, frozen)
}
