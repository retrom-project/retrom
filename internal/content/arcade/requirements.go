package arcade

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/importing"
)

// RequirementReader supplies only the selected DAT's facts, without workflow or installation state.
type RequirementReader interface {
	RelationReader
	ArcadeRequirements(context.Context, string, string) (CatalogRequirements, error)
}

// Requirements separates archive-owned bytes from proven inheritance within its DAT closure.
// Installed BIOS affects readiness, never whether an otherwise correct Parent can be attached.
type Requirements struct {
	Owned     []ROMRequirement
	Inherited []ROMRequirement
	HasDisk   bool
}

func LoadRequirements(ctx context.Context, reader RequirementReader, datID, machine string) (Requirements, error) {
	nodes, cyclic, err := LoadClosure(ctx, reader, datID, machine)
	if err != nil {
		return Requirements{}, err
	}
	if cyclic {
		return Requirements{}, ErrInvalid
	}
	facts := make(map[string]CatalogRequirements, len(nodes))
	for _, node := range nodes {
		value, readErr := reader.ArcadeRequirements(ctx, datID, node.Machine)
		if readErr != nil {
			return Requirements{}, fmt.Errorf("read arcade entry sources: %w", readErr)
		}
		facts[node.Machine] = value
	}
	result := Requirements{HasDisk: facts[machine].HasDisk}
	for _, rom := range SelectRequirements(facts[machine]) {
		if inheritedRequirement(rom, nodes[1:], facts) {
			result.Inherited = append(result.Inherited, rom)
		} else {
			result.Owned = append(result.Owned, rom)
		}
	}
	return result, nil
}

func inheritedRequirement(rom ROMRequirement, ancestors []ClosureNode, facts map[string]CatalogRequirements) bool {
	if rom.MergeName == nil {
		return false
	}
	for _, node := range ancestors {
		for _, source := range SelectRequirements(facts[node.Machine]) {
			if importing.ASCIICaseFold(*rom.MergeName) == importing.ASCIICaseFold(source.Name) && sameROMIdentity(rom, source) {
				return true
			}
		}
	}
	return false
}

func sameROMIdentity(rom, source ROMRequirement) bool {
	if rom.Size != source.Size {
		return false
	}
	// Every hash required by the inheriting row must be proven by the declared source.
	if rom.CRC32 != nil && (source.CRC32 == nil || !strings.EqualFold(*rom.CRC32, *source.CRC32)) {
		return false
	}
	if rom.SHA1 != nil && (source.SHA1 == nil || !strings.EqualFold(*rom.SHA1, *source.SHA1)) {
		return false
	}
	return rom.CRC32 != nil || rom.SHA1 != nil
}

// Archive includes inherited entries when supplied, so bad copies cannot hide behind another node.
func (requirements Requirements) Archive(entries map[string]importing.ArchiveEntry) []ROMRequirement {
	result := append([]ROMRequirement(nil), requirements.Owned...)
	present := make(map[string]bool, len(entries))
	for name := range entries {
		present[importing.ASCIICaseFold(name)] = true
	}
	for _, rom := range requirements.Inherited {
		if present[importing.ASCIICaseFold(rom.Name)] {
			result = append(result, rom)
		}
	}
	return result
}
