// Package runtimecatalog owns Retrom's platform-to-Provider target policy.
package runtimecatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
)

var (
	ErrCatalogInvalid   = errors.New("RUNTIME_TARGET_CATALOG_INVALID")
	ErrBindingNotFound  = errors.New("RUNTIME_TARGET_BINDING_NOT_FOUND")
	ErrBindingAmbiguous = errors.New("RUNTIME_TARGET_BINDING_AMBIGUOUS")
	ErrBindingDisabled  = errors.New("RUNTIME_TARGET_BINDING_DISABLED")
	errTrailingJSON     = errors.New("trailing JSON")
	kebabPattern        = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	identifierPattern   = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
	profilePattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	versionedSemantic   = regexp.MustCompile(`_V[0-9]+$`)
)

type ResolveRequest struct {
	PlatformID       string
	CoreID           string
	ContentKind      string
	DetectorEvidence map[string]bool
}

type Catalog struct {
	SchemaVersion int         `json:"schemaVersion"`
	Definitions   Definitions `json:"definitions"`
	Bindings      []Binding   `json:"bindings"`
}

type Binding struct {
	ID                   string   `json:"id"`
	CoreID               string   `json:"coreId"`
	ProviderID           string   `json:"providerId"`
	TargetID             string   `json:"targetId"`
	PlatformIDs          []string `json:"platformIds"`
	AcceptedContentKinds []string `json:"acceptedContentKinds"`
	DetectorProfile      string   `json:"detectorProfile"`
	LaunchPolicy         string   `json:"launchPolicy"`
}

func ParseCatalog(contents []byte) (Catalog, error) {
	var result Catalog
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Catalog{}, invalidCatalog(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Catalog{}, invalidCatalog(errTrailingJSON)
	}
	if result.SchemaVersion != 1 || len(result.Bindings) == 0 {
		return Catalog{}, ErrCatalogInvalid
	}
	identities := make(map[string]bool, len(result.Bindings))
	bindingIDs := make(map[string]bool, len(result.Bindings))
	previous := ""
	for _, binding := range result.Bindings {
		identity := binding.ProviderID + "\x00" + binding.TargetID
		if !validBinding(binding) || bindingIDs[binding.ID] || identities[identity] ||
			previous != "" && previous >= identity {
			return Catalog{}, ErrCatalogInvalid
		}
		bindingIDs[binding.ID] = true
		identities[identity] = true
		previous = identity
	}
	if err := ValidateDefinitions(result); err != nil {
		return Catalog{}, err
	}
	return result, nil
}

func ValidateManifestBindings(catalog Catalog, targetExists func(providerID, targetID string) bool) error {
	for _, binding := range catalog.Bindings {
		if !targetExists(binding.ProviderID, binding.TargetID) {
			return fmt.Errorf("%w: target %s/%s", ErrCatalogInvalid, binding.ProviderID, binding.TargetID)
		}
	}
	return nil
}

// Resolve maps Host-owned product identity and detector evidence to exactly one
// Provider target. Detector evidence is mandatory only for the intentionally
// ambiguous RPG Maker product core.
func Resolve(catalog Catalog, request ResolveRequest) (Binding, error) {
	if !identifierPattern.MatchString(request.PlatformID) || !identifierPattern.MatchString(request.CoreID) ||
		!profilePattern.MatchString(request.ContentKind) {
		return Binding{}, ErrBindingNotFound
	}
	candidates := make([]Binding, 0, 1)
	for _, binding := range catalog.Bindings {
		if binding.CoreID == request.CoreID && contains(binding.PlatformIDs, request.PlatformID) &&
			contains(binding.AcceptedContentKinds, request.ContentKind) {
			candidates = append(candidates, binding)
		}
	}
	if len(candidates) == 0 {
		return Binding{}, ErrBindingNotFound
	}

	var selected Binding
	var err error
	if request.CoreID == "rpgmaker" {
		selected, err = resolveRPGMaker(candidates, request.DetectorEvidence)
	} else {
		selected, err = resolveExact(candidates, request.DetectorEvidence)
	}
	if err != nil {
		return Binding{}, err
	}
	if selected.LaunchPolicy == "DISABLED" {
		return Binding{}, fmt.Errorf("%w: %s", ErrBindingDisabled, selected.ID)
	}
	return selected, nil
}

func resolveRPGMaker(candidates []Binding, evidence map[string]bool) (Binding, error) {
	if positiveEvidenceCount(evidence) != 1 {
		return Binding{}, ErrBindingAmbiguous
	}
	matches := make([]Binding, 0, 1)
	for _, candidate := range candidates {
		if evidence[candidate.DetectorProfile] {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return Binding{}, ErrBindingNotFound
	}
	if len(matches) != 1 {
		return Binding{}, ErrBindingAmbiguous
	}
	return matches[0], nil
}

func resolveExact(candidates []Binding, evidence map[string]bool) (Binding, error) {
	if positiveEvidenceCount(evidence) != 0 || len(candidates) != 1 {
		return Binding{}, ErrBindingAmbiguous
	}
	return candidates[0], nil
}

func positiveEvidenceCount(evidence map[string]bool) int {
	count := 0
	for _, matched := range evidence {
		if matched {
			count++
		}
	}
	return count
}

func validBinding(value Binding) bool {
	if !validBindingIdentity(value) || !validBindingSemantics(value) {
		return false
	}
	return validLaunchPolicy(value.LaunchPolicy) && validStrategy(value)
}

func validBindingIdentity(value Binding) bool {
	if !kebabPattern.MatchString(value.ID) || !identifierPattern.MatchString(value.CoreID) ||
		!kebabPattern.MatchString(value.ProviderID) || !identifierPattern.MatchString(value.TargetID) ||
		!profilePattern.MatchString(value.DetectorProfile) ||
		!sortedMatches(value.PlatformIDs, identifierPattern) ||
		!sortedMatches(value.AcceptedContentKinds, profilePattern) {
		return false
	}
	return true
}

func validBindingSemantics(value Binding) bool {
	semanticValues := append([]string{
		value.DetectorProfile, value.LaunchPolicy,
	},
		value.AcceptedContentKinds...)
	for _, semanticValue := range semanticValues {
		if versionedSemantic.MatchString(semanticValue) {
			return false
		}
	}
	return true
}

func validLaunchPolicy(value string) bool {
	return value == "SUPPORTED" || value == "EXPERIMENTAL" || value == "DISABLED"
}

func sortedMatches(values []string, pattern *regexp.Regexp) bool {
	if len(values) == 0 || !sort.StringsAreSorted(values) {
		return false
	}
	for index, value := range values {
		if !pattern.MatchString(value) || index > 0 && values[index-1] == value {
			return false
		}
	}
	return true
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func invalidCatalog(err error) error {
	return fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
}
