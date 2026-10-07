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
	SourceDirectory struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	SourceEntry struct {
		Key       string `json:"key"`
		Name      string `json:"name"`
		Path      string `json:"path"`
		GameCount int64  `json:"gameCount"`
	}
	SourceInput struct {
		Path   string `json:"path"`
		Format string `json:"format"`
	}
	SourceMapping struct {
		SourceKey   string   `json:"sourceKey"`
		DirectoryID string   `json:"platformInstanceId"`
		TagIDs      []string `json:"tagIds"`
	}
	GameScanInput struct {
		Path     string          `json:"path"`
		Format   string          `json:"format"`
		Mappings []SourceMapping `json:"mappings"`
	}
)

type (
	BiosScanInput struct {
		Path        string   `json:"path"`
		PlatformIDs []string `json:"platformIds"`
		CoreIDs     []string `json:"coreIds"`
	}
	BiosValidationRequirement struct {
		CoreID    string  `json:"coreId"`
		SizeBytes *int64  `json:"sizeBytes"`
		SHA256    *string `json:"sha256"`
		MD5       *string `json:"md5"`
	}
	BiosRequirement struct {
		Requirements []BiosValidationRequirement `json:"requirements"`
		Key          string                      `json:"key"`
		Name         string                      `json:"name"`
		PlatformIDs  []string                    `json:"platformIds"`
		CoreIDs      []string                    `json:"coreIds"`
		Required     bool                        `json:"required"`
		Installed    bool                        `json:"installed"`
		Filename     string                      `json:"filename"`
		SizeBytes    int64                       `json:"sizeBytes"`
		SHA256       string                      `json:"sha256"`
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
