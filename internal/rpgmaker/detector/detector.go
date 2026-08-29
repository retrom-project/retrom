package detector

import (
	"errors"
	"sort"
)

var coreGenerations = map[string]Generation{
	"rpgmaker_2000":   RPG2000,
	"rpgmaker_2003":   RPG2003,
	"rpgmaker_xp":     RPGXP,
	"rpgmaker_vx":     RPGVX,
	"rpgmaker_vx_ace": RPGVXAce,
	"rpgmaker_mv":     RPGMV,
	"rpgmaker_mz":     RPGMZ,
}

const VirtualCoreID = "rpgmaker"

type evidence struct {
	generation      Generation
	family          string
	markers         []string
	engineVersion   string
	requirements    []Requirement
	rtpDependencies []RTPDependency
	selfContained   bool
}

func GenerationForCore(coreID string) (Generation, error) {
	generation, exists := coreGenerations[coreID]
	if !exists {
		return "", newError(CodeCoreUnsupported, "core is not a supported RPG Maker version core", nil)
	}
	return generation, nil
}

func CoreForGeneration(generation Generation) (string, error) {
	for coreID, candidate := range coreGenerations {
		if candidate == generation {
			return coreID, nil
		}
	}
	return "", newError(CodeGenerationUnsupported, "generation has no internal runtime core", nil)
}

func Detect(coreID string, source FileIndex) (Profile, error) {
	if coreID == VirtualCoreID {
		return detectVirtualCore(source)
	}
	expected, err := GenerationForCore(coreID)
	if err != nil {
		return Profile{}, err
	}
	files, err := newCatalog(source)
	if err != nil {
		return Profile{}, withExpected(err, expected)
	}
	evidenceSet, err := collectEvidence(files)
	if err != nil {
		return Profile{}, withExpected(err, expected)
	}
	if len(evidenceSet) == 0 {
		unsupported := newError(CodeGenerationUnsupported, "no supported generation signature", nil)
		return Profile{}, withExpected(unsupported, expected)
	}
	if len(evidenceSet) > 1 {
		markerPaths := mergeMarkers(evidenceSet)
		detectionError := newError(CodeGenerationAmbiguous, "multiple generation signatures are complete", nil)
		detectionError.MarkerPaths = markerPaths
		return Profile{}, withExpected(detectionError, expected)
	}
	return resolveEvidence(coreID, expected, evidenceSet[0])
}

func detectVirtualCore(source FileIndex) (Profile, error) {
	files, err := newCatalog(source)
	if err != nil {
		return Profile{}, err
	}
	evidenceSet, err := collectEvidence(files)
	if err != nil {
		return Profile{}, err
	}
	if len(evidenceSet) == 0 {
		return Profile{}, newError(CodeGenerationUnsupported, "no supported generation signature", nil)
	}
	if len(evidenceSet) > 1 {
		detectionError := newError(CodeGenerationAmbiguous, "multiple generation signatures are complete", nil)
		detectionError.MarkerPaths = mergeMarkers(evidenceSet)
		return Profile{}, detectionError
	}
	found := evidenceSet[0]
	if found.generation == "" {
		detectionError := newError(CodeGenerationAmbiguous, "RPG Maker 2000/2003 generation is not distinguishable", nil)
		detectionError.EvidenceFamily = found.family
		detectionError.MarkerPaths = append([]string(nil), found.markers...)
		return Profile{}, detectionError
	}
	coreID, err := CoreForGeneration(found.generation)
	if err != nil {
		return Profile{}, err
	}
	return profileFromEvidence(coreID, found.generation, found, Matched, ConfidenceExact), nil
}

func collectEvidence(files *catalog) ([]evidence, error) {
	parsers := []func(*catalog) ([]evidence, error){detectRPG2K, detectRGSS, detectWeb}
	result := make([]evidence, 0, 2)
	for _, parser := range parsers {
		matches, err := parser(files)
		if err != nil {
			return nil, err
		}
		result = append(result, matches...)
	}
	return result, nil
}

func resolveEvidence(coreID string, expected Generation, found evidence) (Profile, error) {
	if found.family == FamilyRPG2K {
		return resolveRPG2KEvidence(coreID, expected, found)
	}
	if found.generation != expected {
		detectionError := newError(CodeSelectedCoreMismatch, "exact generation conflicts with selected core", nil)
		detectionError.EvidenceGeneration = generationPointer(found.generation)
		detectionError.MarkerPaths = append([]string(nil), found.markers...)
		return Profile{}, withExpected(detectionError, expected)
	}
	return profileFromEvidence(coreID, expected, found, Matched, ConfidenceExact), nil
}

func resolveRPG2KEvidence(coreID string, expected Generation, found evidence) (Profile, error) {
	if expected != RPG2000 && expected != RPG2003 {
		detectionError := newError(CodeSelectedCoreMismatch, "RPG2K family conflicts with selected core", nil)
		detectionError.EvidenceFamily = FamilyRPG2K
		detectionError.MarkerPaths = append([]string(nil), found.markers...)
		return Profile{}, withExpected(detectionError, expected)
	}
	if found.generation == "" {
		return profileFromEvidence(coreID, expected, found, FamilyOnly, ConfidenceFamilyOnly), nil
	}
	if found.generation != expected {
		detectionError := newError(CodeSelectedCoreMismatch, "exact RPG2K generation conflicts with selected core", nil)
		detectionError.EvidenceFamily = FamilyRPG2K
		detectionError.EvidenceGeneration = generationPointer(found.generation)
		detectionError.MarkerPaths = append([]string(nil), found.markers...)
		return Profile{}, withExpected(detectionError, expected)
	}
	return profileFromEvidence(coreID, expected, found, Matched, ConfidenceExact), nil
}

func profileFromEvidence(
	coreID string,
	expected Generation,
	found evidence,
	status Status,
	confidence Confidence,
) Profile {
	markers := append([]string(nil), found.markers...)
	requirements := append([]Requirement(nil), found.requirements...)
	sort.Strings(markers)
	sort.Slice(requirements, func(left, right int) bool { return requirements[left] < requirements[right] })
	profile := Profile{
		SelectedCoreID: coreID, Status: status, ExpectedGeneration: expected,
		EvidenceFamily: found.family, EvidenceConfidence: confidence, MarkerPaths: markers,
		EngineVersion: found.engineVersion, Requirements: requirements,
		RTPDependencies: append([]RTPDependency(nil), found.rtpDependencies...),
		SelfContained:   found.selfContained,
	}
	if found.generation != "" {
		profile.EvidenceGeneration = generationPointer(found.generation)
	}
	return profile
}

func withExpected(err error, expected Generation) error {
	var detectionError *Error
	if errors.As(err, &detectionError) {
		detectionError.ExpectedGeneration = expected
	}
	return err
}

func generationPointer(generation Generation) *Generation {
	return &generation
}

func mergeMarkers(evidenceSet []evidence) []string {
	seen := make(map[string]struct{})
	for _, found := range evidenceSet {
		for _, marker := range found.markers {
			seen[marker] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for marker := range seen {
		result = append(result, marker)
	}
	sort.Strings(result)
	return result
}
