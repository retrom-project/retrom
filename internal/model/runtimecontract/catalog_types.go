// Package runtimecontract defines stable shared Host runtime values.
package runtimecontract

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
