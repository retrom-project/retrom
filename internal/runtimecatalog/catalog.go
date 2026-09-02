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
	ErrCatalogInvalid = errors.New("RUNTIME_TARGET_CATALOG_INVALID")
	kebabPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	identifierPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
	profilePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
)

type Catalog struct {
	SchemaVersion  int       `json:"schemaVersion"`
	CatalogVersion int       `json:"catalogVersion"`
	Bindings       []Binding `json:"bindings"`
}

type Binding struct {
	ID                   string   `json:"id"`
	CoreID               string   `json:"coreId"`
	ProviderID           string   `json:"providerId"`
	TargetID             string   `json:"targetId"`
	PlatformIDs          []string `json:"platformIds"`
	AcceptedContentKinds []string `json:"acceptedContentKinds"`
	DetectorProfile      string   `json:"detectorProfile"`
	DeliveryProfile      string   `json:"deliveryProfile"`
	LaunchPolicy         string   `json:"launchPolicy"`
	ReviewPolicy         string   `json:"reviewPolicy"`
}

func ParseCatalog(contents []byte) (Catalog, error) {
	var result Catalog
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Catalog{}, invalidCatalog(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Catalog{}, invalidCatalog(errors.New("trailing JSON"))
	}
	if result.SchemaVersion != 1 || result.CatalogVersion < 1 || len(result.Bindings) == 0 {
		return Catalog{}, ErrCatalogInvalid
	}
	identities := make(map[string]bool, len(result.Bindings))
	bindingIDs := make(map[string]bool, len(result.Bindings))
	previous := ""
	for _, binding := range result.Bindings {
		identity := binding.ProviderID + "\x00" + binding.TargetID
		if !validBinding(binding) || bindingIDs[binding.ID] || identities[identity] || previous != "" && previous >= identity {
			return Catalog{}, ErrCatalogInvalid
		}
		bindingIDs[binding.ID] = true
		identities[identity] = true
		previous = identity
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

func validBinding(value Binding) bool {
	if !kebabPattern.MatchString(value.ID) || !identifierPattern.MatchString(value.CoreID) ||
		!kebabPattern.MatchString(value.ProviderID) || !identifierPattern.MatchString(value.TargetID) ||
		!profilePattern.MatchString(value.DetectorProfile) ||
		!profilePattern.MatchString(value.DeliveryProfile) || !sortedMatches(value.PlatformIDs, identifierPattern) ||
		!sortedMatches(value.AcceptedContentKinds, profilePattern) {
		return false
	}
	if value.LaunchPolicy != "SUPPORTED" && value.LaunchPolicy != "EXPERIMENTAL" && value.LaunchPolicy != "DISABLED" {
		return false
	}
	if value.ReviewPolicy != "NONE" && value.ReviewPolicy != "RPG_RUNTIME_VALIDATION_V1" {
		return false
	}
	return true
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

func invalidCatalog(err error) error {
	return fmt.Errorf("%w: %v", ErrCatalogInvalid, err)
}
