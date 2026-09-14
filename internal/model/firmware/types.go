package firmware

import (
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
)

type InstallRequest struct {
	UploadFileID string `json:"uploadFileId"`
}

type Installation struct {
	InstallationID              string         `json:"installationId"`
	RequirementID               string         `json:"requirementId"`
	Status                      string         `json:"status"`
	Active                      bool           `json:"active"`
	ValidatedRequirementVersion int64          `json:"validatedRequirementVersion"`
	ValidationDetails           map[string]any `json:"validationDetails"`
	CreatedAtMS                 int64          `json:"createdAtMs"`
}

type ArchiveInspection struct {
	RequirementID      string                            `json:"requirementId"`
	LogicalName        string                            `json:"logicalName"`
	InstallationID     string                            `json:"installationId"`
	InstallationStatus string                            `json:"installationStatus"`
	Entries            []firmware.ArchiveEntryComparison `json:"entries"`
}

type ServerInstallRequest struct {
	ServerImportID     string
	JobID              string
	WorkerID           string
	ExecutionNo        int64
	CandidateID        string
	RequirementID      string
	RequirementVersion int64
	ProviderID         string
	TargetID           string
	SourceVersion      string
	CatalogDigest      string
	SourceKind         string
	ArchiveMembersJSON *string
	LogicalName        string
	OriginalFilename   string
	Metadata           blobstore.Metadata
	Status             string
	MatchMethod        string
	Details            map[string]any
	ArchiveEntries     []importing.ArchiveEntry
	ReplaceIfBetter    bool
	StaticExpectation  *firmware.StaticExpectation
	StaticEvaluation   *firmware.StaticEvaluation
	DATExpectedEntries []firmware.ExpectedDATEntry
	DATEvaluation      *firmware.DATEvaluation
}

type ServerInstallResult struct {
	Outcome                string
	PreviousInstallationID string
	NewInstallationID      string
	OutcomeCode            string
}
