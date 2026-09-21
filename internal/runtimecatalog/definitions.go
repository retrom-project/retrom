package runtimecatalog

import (
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var packKindPattern = regexp.MustCompile(`^[A-Za-z0-9_]{2,64}$`)

func NormalizePackName(value string) string {
	return cases.Fold().String(norm.NFKC.String(strings.TrimSpace(value)))
}

// Definitions are Host-owned product data, projected alongside Provider targets.
// User-created directories, configuration and installations are not definitions.
type Definitions struct {
	Platforms    []PlatformDefinition  `json:"platforms"`
	Cores        []CoreDefinition      `json:"cores"`
	ContentKinds []string              `json:"contentKinds"`
	AssetPacks   []AssetPackDefinition `json:"assetPacks"`
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

type AssetPackDefinition struct {
	ID                     string `json:"id"`
	Kind                   string `json:"kind"`
	Generation             string `json:"generation"`
	DeclaredName           string `json:"declaredName"`
	NormalizedDeclaredName string `json:"normalizedDeclaredName"`
	DisplayName            string `json:"displayName"`
	RequiredLayoutVersion  string `json:"requiredLayoutVersion"`
	Enabled                bool   `json:"enabled"`
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
	return validatePackDefinitions(definitions.AssetPacks)
}

func validatePackDefinitions(packs []AssetPackDefinition) error {
	previous := ""
	identities := make(map[string]bool, len(packs))
	for _, pack := range packs {
		identity := pack.Generation + "\x00" + pack.NormalizedDeclaredName
		if !identifierPattern.MatchString(pack.ID) || pack.ID <= previous || identities[identity] ||
			!packKindPattern.MatchString(pack.Kind) || !profilePattern.MatchString(pack.Generation) ||
			!validProductName(pack.DisplayName) || len(pack.DeclaredName) < 1 || len(pack.DeclaredName) > 512 ||
			strings.ContainsRune(pack.DeclaredName, 0) || pack.NormalizedDeclaredName != NormalizePackName(pack.DeclaredName) ||
			!ValidPackLayout(pack.RequiredLayoutVersion, pack.Generation) {
			return ErrCatalogInvalid
		}
		identities[identity], previous = true, pack.ID
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
