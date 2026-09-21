// Package emulationstationmeta parses and projects EmulationStation
// gamelist.xml documents without accessing the filesystem, database, network,
// clock, or task scheduler.
package emulationstationmeta

import "errors"

const (
	MaxGameListBytes        = 8 << 20
	MaxXMLDepth             = 16
	MaxXMLAttributes        = 16
	MaxXMLTokenBytes        = 1 << 20
	MaxXMLTokens            = 1_000_000
	MaxGameFields           = 128
	MaxGames                = 100_000
	MaxWarnings             = 64
	MaxTitleRunes           = 200
	MaxDescriptionRunes     = 20_000
	MaxMetadataRunes        = 500
	MaxDeclaredPathBytes    = 4096
	MaxDeclaredSegmentBytes = 255
	MaxIgnoredFieldNames    = 64
)

var (
	ErrTooLarge      = errors.New("EMULATIONSTATION_GAMELIST_TOO_LARGE")
	ErrInvalidUTF8   = errors.New("EMULATIONSTATION_GAMELIST_INVALID_UTF8")
	ErrInvalidXML    = errors.New("EMULATIONSTATION_XML_INVALID")
	ErrInvalidRoot   = errors.New("EMULATIONSTATION_XML_ROOT_INVALID")
	ErrLimitExceeded = errors.New("EMULATIONSTATION_XML_LIMIT_EXCEEDED")
	ErrPathInvalid   = errors.New("EMULATIONSTATION_PATH_INVALID")
)

const (
	CodePathMissing   = "EMULATIONSTATION_GAME_PATH_MISSING"
	CodePathAmbiguous = "EMULATIONSTATION_GAME_PATH_AMBIGUOUS"
	CodePathInvalid   = "EMULATIONSTATION_PATH_INVALID"

	WarningDuplicateField = "DUPLICATE_SINGLETON_FIELD"
	WarningFieldIgnored   = "FIELD_IGNORED"
	WarningFieldInvalid   = "FIELD_VALUE_INVALID"
	WarningFieldStructure = "FIELD_STRUCTURE_INVALID"
	WarningFieldTruncated = "FIELD_TRUNCATED"
	WarningPlayerRange    = "PLAYER_RANGE_NORMALIZED"
	WarningExecutionField = "EMULATIONSTATION_EXECUTION_FIELD_IGNORED"
	WarningLimitReached   = "WARNING_LIMIT_REACHED"
)

type Warning struct {
	Code           string `json:"code"`
	Field          string `json:"field,omitempty"`
	PathKind       string `json:"pathKind,omitempty"`
	OmittedCount   int    `json:"omittedCount,omitempty"`
	OriginalLength int    `json:"originalLength,omitempty"`
	RetainedLength int    `json:"retainedLength,omitempty"`
}

type Metadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Developer     string `json:"developer"`
	Publisher     string `json:"publisher"`
	Genre         string `json:"genre"`
	Players       *int   `json:"players"`
	ReleaseYear   *int   `json:"releaseYear"`
}

type SourceFlags struct {
	Hidden  bool `json:"hidden"`
	Adult   bool `json:"adult"`
	KidGame bool `json:"kidGame"`
}

type AssetReference struct {
	RelativePath     string `json:"relativePath"`
	ResolutionMethod string `json:"resolutionMethod"`
}

type AssetReferences struct {
	Cover *AssetReference `json:"cover,omitempty"`
	Video *AssetReference `json:"video,omitempty"`
}

type Game struct {
	Ordinal     int             `json:"ordinal"`
	Path        string          `json:"path,omitempty"`
	Metadata    Metadata        `json:"metadata"`
	SourceFlags SourceFlags     `json:"sourceFlags"`
	Assets      AssetReferences `json:"assets"`
	Warnings    []Warning       `json:"warnings,omitempty"`
	BlockedCode string          `json:"blockedCode,omitempty"`
}

type Document struct {
	Games                  []Game   `json:"games"`
	FolderEntryCount       int      `json:"folderEntryCount"`
	ProviderPresent        bool     `json:"providerPresent"`
	IgnoredFields          []string `json:"ignoredFields,omitempty"`
	IgnoredFieldOtherCount int      `json:"ignoredFieldOtherCount"`
}
