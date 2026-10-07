package model

type Scan struct {
	ID             string  `json:"id"`
	ScanType       string  `json:"scanType"`
	Status         string  `json:"status"`
	TotalCount     int64   `json:"totalCount"`
	TotalKnown     bool    `json:"totalKnown"`
	ProcessedCount int64   `json:"processedCount"`
	ImportedCount  int64   `json:"importedCount"`
	SkippedCount   int64   `json:"skippedCount"`
	FailedCount    int64   `json:"failedCount"`
	CreatedAtMs    int64   `json:"createdAtMs"`
	UpdatedAtMs    int64   `json:"updatedAtMs"`
	Error          *string `json:"error"`
}
type (
	Root struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"-"`
	}
	SourceDirectory struct {
		RelativePath string `json:"relativePath"`
		Name         string `json:"name"`
	}
	SourceEntry struct {
		Key          string `json:"key"`
		Name         string `json:"name"`
		RelativePath string `json:"relativePath"`
		GameCount    int64  `json:"gameCount"`
	}
	SourceInput struct {
		RootID       string `json:"rootId"`
		RelativePath string `json:"relativePath"`
		Format       string `json:"format"`
	}
	SourceMapping struct {
		SourceKey   string   `json:"sourceKey"`
		DirectoryID string   `json:"platformInstanceId"`
		TagIDs      []string `json:"tagIds"`
	}
	GameScanInput struct {
		RootID       string          `json:"rootId"`
		RelativePath string          `json:"relativePath"`
		Format       string          `json:"format"`
		Mappings     []SourceMapping `json:"mappings"`
	}
)

type (
	BiosScanInput struct {
		RootID       string   `json:"rootId"`
		RelativePath string   `json:"relativePath"`
		PlatformIDs  []string `json:"platformIds"`
		CoreIDs      []string `json:"coreIds"`
	}
	BiosRequirement struct {
		Key         string   `json:"key"`
		Name        string   `json:"name"`
		PlatformIDs []string `json:"platformIds"`
		CoreIDs     []string `json:"coreIds"`
		Required    bool     `json:"required"`
		Installed   bool     `json:"installed"`
		Filename    string   `json:"filename"`
		SizeBytes   int64    `json:"sizeBytes"`
		SHA256      string   `json:"sha256"`
	}
)

type BiosFile struct {
	ID             string
	RequirementKey string
	Filename       string
	StorageKey     string
	SizeBytes      int64
	SHA256         string
}
