package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"

	"retrom/internal/rpgmaker/detector"
	"retrom/internal/runtimecatalog"
)

var (
	ErrMissing   = errors.New("RPG_RUNTIME_PACK_MISSING")
	ErrAmbiguous = errors.New("RPG_RUNTIME_PACK_AMBIGUOUS")
	ErrInvalid   = errors.New("RPG_RUNTIME_PACK_INVALID")
)

type Definition struct {
	ID                     string
	Generation             detector.Generation
	DeclaredName           string
	NormalizedDeclaredName string
	Enabled                bool
}

type Installation struct {
	ID           string
	DefinitionID string
	FilesDigest  string
	Status       string
	Deleted      bool
}

type Requirement struct {
	Slot           int
	DeclaredName   string
	NormalizedName string
}

type Selection struct {
	Slot           int
	InstallationID string
}

type Binding struct {
	Slot                   int    `json:"slot"`
	DeclaredName           string `json:"declaredName"`
	NormalizedDeclaredName string `json:"normalizedDeclaredName"`
	DefinitionID           string `json:"definitionId"`
	InstallationID         string `json:"installationId"`
	FilesDigest            string `json:"filesDigest"`
}

type Resolution struct {
	Bindings         []Binding
	DependencySHA256 string
}

func Resolve(
	generation detector.Generation,
	selfContained bool,
	selfContainedOverride bool,
	requirements []Requirement,
	definitions []Definition,
	installations []Installation,
	selections []Selection,
) (Resolution, error) {
	if !supportedGeneration(generation) {
		return Resolution{}, ErrInvalid
	}
	selectionBySlot, err := validateSelections(generation, selections)
	if err != nil {
		return Resolution{}, err
	}
	resolvedRequirements, err := requirementsForGeneration(
		generation, selfContained, selfContainedOverride, requirements,
	)
	if err != nil {
		return Resolution{}, err
	}
	if len(selectionBySlot) > len(resolvedRequirements) {
		return Resolution{}, ErrInvalid
	}
	bindings := make([]Binding, 0, len(resolvedRequirements))
	for _, requirement := range resolvedRequirements {
		definition, err := matchDefinition(generation, requirement, definitions)
		if err != nil {
			return Resolution{}, err
		}
		installation, err := matchInstallation(definition.ID, selectionBySlot[requirement.Slot], installations)
		if err != nil {
			return Resolution{}, err
		}
		delete(selectionBySlot, requirement.Slot)
		bindings = append(bindings, Binding{
			Slot: requirement.Slot, DeclaredName: requirement.DeclaredName,
			NormalizedDeclaredName: requirement.NormalizedName, DefinitionID: definition.ID,
			InstallationID: installation.ID, FilesDigest: installation.FilesDigest,
		})
	}
	if len(selectionBySlot) != 0 {
		return Resolution{}, ErrInvalid
	}
	sort.Slice(bindings, func(left, right int) bool { return bindings[left].Slot < bindings[right].Slot })
	contents, err := json.Marshal(map[string]any{"bindings": bindings, "schemaVersion": 1})
	if err != nil {
		return Resolution{}, ErrInvalid
	}
	digest := sha256.Sum256(contents)
	return Resolution{Bindings: bindings, DependencySHA256: hex.EncodeToString(digest[:])}, nil
}

func requirementsForGeneration(
	generation detector.Generation,
	selfContained, override bool,
	requirements []Requirement,
) ([]Requirement, error) {
	switch generation {
	case detector.RPGMV, detector.RPGMZ:
		if len(requirements) != 0 || override {
			return nil, ErrInvalid
		}
		return nil, nil
	case detector.RPG2000, detector.RPG2003:
		if len(requirements) != 0 || selfContained || override {
			return nil, nil
		}
		declaredName := map[detector.Generation]string{
			detector.RPG2000: "RPG2000_RTP", detector.RPG2003: "RPG2003_RTP",
		}[generation]
		return []Requirement{{Slot: 0, DeclaredName: declaredName, NormalizedName: NormalizeDeclaredName(declaredName)}}, nil
	case detector.RPGXP, detector.RPGVX, detector.RPGVXAce:
		return rgssRequirements(selfContained, override, requirements)
	default:
		return nil, ErrInvalid
	}
}

func rgssRequirements(selfContained, override bool, requirements []Requirement) ([]Requirement, error) {
	if selfContained || override {
		return nil, ErrInvalid
	}
	result := append([]Requirement(nil), requirements...)
	sort.Slice(result, func(left, right int) bool { return result[left].Slot < result[right].Slot })
	seen := make(map[int]struct{}, len(result))
	for index := range result {
		requirement := &result[index]
		if requirement.Slot < 1 || requirement.Slot > 3 || requirement.DeclaredName == "" {
			return nil, ErrInvalid
		}
		if _, duplicate := seen[requirement.Slot]; duplicate {
			return nil, ErrInvalid
		}
		seen[requirement.Slot] = struct{}{}
		normalized := NormalizeDeclaredName(requirement.DeclaredName)
		if requirement.NormalizedName != "" && requirement.NormalizedName != normalized {
			return nil, ErrInvalid
		}
		requirement.NormalizedName = normalized
	}
	return result, nil
}

func matchDefinition(
	generation detector.Generation,
	requirement Requirement,
	definitions []Definition,
) (Definition, error) {
	matches := make([]Definition, 0, 1)
	for _, definition := range definitions {
		if definition.Enabled && definition.Generation == generation &&
			definition.NormalizedDeclaredName == requirement.NormalizedName &&
			definition.NormalizedDeclaredName == NormalizeDeclaredName(definition.DeclaredName) {
			matches = append(matches, definition)
		}
	}
	if len(matches) == 0 {
		return Definition{}, ErrMissing
	}
	if len(matches) > 1 {
		return Definition{}, ErrAmbiguous
	}
	return matches[0], nil
}

func matchInstallation(
	definitionID, selectedID string,
	installations []Installation,
) (Installation, error) {
	candidates := make([]Installation, 0)
	for _, installation := range installations {
		if installation.DefinitionID == definitionID && installation.Status == "READY" &&
			!installation.Deleted && validDigest(installation.FilesDigest) && validUUID(installation.ID) {
			candidates = append(candidates, installation)
		}
	}
	if selectedID != "" {
		for _, candidate := range candidates {
			if candidate.ID == selectedID {
				return candidate, nil
			}
		}
		return Installation{}, ErrInvalid
	}
	if len(candidates) == 0 {
		return Installation{}, ErrMissing
	}
	if len(candidates) > 1 {
		return Installation{}, ErrAmbiguous
	}
	return candidates[0], nil
}

func validateSelections(generation detector.Generation, selections []Selection) (map[int]string, error) {
	if len(selections) > 3 {
		return nil, ErrInvalid
	}
	result := make(map[int]string, len(selections))
	seenInstallations := make(map[string]struct{}, len(selections))
	for _, selection := range selections {
		validSlot := generation == detector.RPG2000 || generation == detector.RPG2003
		if validSlot {
			validSlot = selection.Slot == 0
		} else {
			validSlot = selection.Slot >= 1 && selection.Slot <= 3
		}
		if !validSlot || !validUUID(selection.InstallationID) {
			return nil, ErrInvalid
		}
		if _, duplicate := result[selection.Slot]; duplicate {
			return nil, ErrInvalid
		}
		if _, duplicate := seenInstallations[selection.InstallationID]; duplicate {
			return nil, ErrInvalid
		}
		result[selection.Slot] = selection.InstallationID
		seenInstallations[selection.InstallationID] = struct{}{}
	}
	return result, nil
}

func NormalizeDeclaredName(value string) string {
	return runtimecatalog.NormalizePackName(value)
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func validDigest(value string) bool {
	if len(value) != 64 || value != string([]byte(value)) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == NormalizeDigest(value)
}

func NormalizeDigest(value string) string {
	result := make([]byte, len(value))
	for index := range value {
		current := value[index]
		if current >= 'A' && current <= 'F' {
			current += 'a' - 'A'
		}
		result[index] = current
	}
	return string(result)
}

func supportedGeneration(value detector.Generation) bool {
	for _, generation := range []detector.Generation{
		detector.RPG2000, detector.RPG2003, detector.RPGXP, detector.RPGVX,
		detector.RPGVXAce, detector.RPGMV, detector.RPGMZ,
	} {
		if value == generation {
			return true
		}
	}
	return false
}
