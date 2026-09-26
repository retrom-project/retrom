package runtimecatalog

import (
	"strings"
)

// Definitions are Host-owned product data, projected alongside Provider targets.
// User-created directories, configuration and installations are not definitions.
type Definitions struct {
	Platforms    []PlatformDefinition `json:"platforms"`
	Cores        []CoreDefinition     `json:"cores"`
	ContentKinds []string             `json:"contentKinds"`
}

type PlatformDefinition struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	Enabled   bool   `json:"enabled"`
}

type CoreDefinition struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func ValidateDefinitions(catalog Catalog) error {
	definitions := catalog.Definitions
	if len(definitions.Platforms) == 0 || len(definitions.Cores) == 0 ||
		!sortedMatches(definitions.ContentKinds, profilePattern) {
		return ErrCatalogInvalid
	}
	platforms, cores := platformDefinitionIDs(definitions.Platforms), coreDefinitionIDs(definitions.Cores)
	if platforms == nil || cores == nil {
		return ErrCatalogInvalid
	}
	for _, binding := range catalog.Bindings {
		if !cores[binding.CoreID] {
			return ErrCatalogInvalid
		}
		for _, platformID := range binding.PlatformIDs {
			if !platforms[platformID] {
				return ErrCatalogInvalid
			}
		}
		for _, kind := range binding.AcceptedContentKinds {
			if !contains(definitions.ContentKinds, kind) {
				return ErrCatalogInvalid
			}
		}
	}
	return nil
}

func validProductName(value string) bool {
	return len(value) >= 1 && len(value) <= 200 && value == strings.TrimSpace(value) && !strings.ContainsRune(value, 0)
}

func platformDefinitionIDs(definitions []PlatformDefinition) map[string]bool {
	ids := make(map[string]bool, len(definitions))
	previous := ""
	for _, definition := range definitions {
		if !identifierPattern.MatchString(definition.ID) || definition.ID <= previous ||
			!validProductName(definition.Name) || definition.SortOrder < 0 {
			return nil
		}
		ids[definition.ID], previous = true, definition.ID
	}
	return ids
}

func coreDefinitionIDs(definitions []CoreDefinition) map[string]bool {
	ids := make(map[string]bool, len(definitions))
	previous := ""
	for _, definition := range definitions {
		if !identifierPattern.MatchString(definition.ID) || definition.ID <= previous ||
			!validProductName(definition.Name) {
			return nil
		}
		ids[definition.ID], previous = true, definition.ID
	}
	return ids
}
