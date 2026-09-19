package hasheous

// Field order preserves the prior map encoder's canonical key ordering.
type normalizedMetadata struct {
	Description   string `json:"description"`
	Developer     string `json:"developer"`
	Genre         string `json:"genre"`
	Players       *int   `json:"players"`
	Publisher     string `json:"publisher"`
	ReleaseYear   *int   `json:"releaseYear"`
	SchemaVersion int    `json:"schemaVersion"`
	Title         string `json:"title"`
}

type normalizedEvidence struct {
	NormalizationYear int      `json:"normalizationYear"`
	NormalizerVersion string   `json:"normalizerVersion"`
	PlatformName      string   `json:"platformName"`
	ProviderGameScore *int64   `json:"providerGameScore"`
	ProviderRomScore  *int64   `json:"providerRomScore"`
	SchemaVersion     int      `json:"schemaVersion"`
	Warnings          []string `json:"warnings"`
}
