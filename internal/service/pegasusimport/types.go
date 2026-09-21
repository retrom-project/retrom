package pegasusimport

import "retrom/internal/service/tagging"

type RootRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CreatedBy struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Counts struct {
	Metadata             int64 `json:"metadata"`
	InvalidMetadata      int64 `json:"invalidMetadata"`
	Collections          int64 `json:"collections"`
	Games                int64 `json:"games"`
	EstimatedSourceBytes int64 `json:"estimatedSourceBytes"`
	MappedCollections    int64 `json:"mappedCollections"`
	SkippedCollections   int64 `json:"skippedCollections"`
	Processable          int64 `json:"processable"`
	Blocked              int64 `json:"blocked"`
	ReviewPending        int64 `json:"reviewPending"`
	Published            int64 `json:"published"`
	ReviewDiscarded      int64 `json:"reviewDiscarded"`
	Existing             int64 `json:"existing"`
	Failed               int64 `json:"failed"`
	Cancelled            int64 `json:"cancelled"`
	MediaWarnings        int64 `json:"mediaWarnings"`
	Covers               int64 `json:"covers"`
	Videos               int64 `json:"videos"`
}

type Summary struct {
	ID                 string    `json:"id"`
	Root               RootRef   `json:"root"`
	SourceRelativePath string    `json:"sourceRelativePath"`
	State              string    `json:"state"`
	Phase              *string   `json:"phase"`
	ScanJobID          string    `json:"scanJobId"`
	ImportJobID        *string   `json:"importJobId"`
	Counts             Counts    `json:"counts"`
	MappingVersion     int64     `json:"mappingVersion"`
	Version            int64     `json:"version"`
	CreatedBy          CreatedBy `json:"createdBy"`
	LastErrorCode      *string   `json:"lastErrorCode"`
	Retryable          bool      `json:"retryable"`
	CreatedAtMS        int64     `json:"createdAtMs"`
	UpdatedAtMS        int64     `json:"updatedAtMs"`
	ExpiresAtMS        int64     `json:"expiresAtMs"`
	CompletedAtMS      *int64    `json:"completedAtMs"`
}

type Collection struct {
	ID                         string              `json:"id"`
	MetadataRelativePath       string              `json:"metadataRelativePath"`
	SegmentOrdinal             int64               `json:"segmentOrdinal"`
	Name                       string              `json:"name"`
	ShortName                  *string             `json:"shortName"`
	Description                string              `json:"description"`
	GameCount                  int64               `json:"gameCount"`
	IssueCount                 int64               `json:"issueCount"`
	MappingAction              *string             `json:"mappingAction"`
	TargetPlatformInstanceID   *string             `json:"targetPlatformInstanceId"`
	TargetPlatformInstanceName *string             `json:"targetPlatformInstanceName"`
	TargetDefaultCoreID        *string             `json:"targetDefaultCoreId"`
	TargetDefaultCoreName      *string             `json:"targetDefaultCoreName"`
	IgnoredRules               []string            `json:"ignoredRules"`
	WarningFields              []string            `json:"warningFields"`
	TagSnapshot                []tagging.Reference `json:"tagSnapshot"`
}

type Mapping struct {
	CollectionID       string   `json:"collectionId"`
	Action             string   `json:"action"`
	PlatformInstanceID string   `json:"platformInstanceId,omitempty"`
	TagIDs             []string `json:"tagIds"`
}

type Item struct {
	ID                         string              `json:"id"`
	Title                      string              `json:"title"`
	CollectionID               *string             `json:"collectionId"`
	CollectionName             *string             `json:"collectionName"`
	TargetPlatformInstanceID   *string             `json:"targetPlatformInstanceId"`
	TargetPlatformInstanceName *string             `json:"targetPlatformInstanceName"`
	MetadataRelativePath       string              `json:"metadataRelativePath"`
	ExecutionState             string              `json:"executionState"`
	PayloadState               string              `json:"payloadState"`
	PayloadReleaseJobID        *string             `json:"payloadReleaseJobId"`
	ContentKind                *string             `json:"contentKind"`
	Media                      ItemMedia           `json:"media"`
	Warnings                   []map[string]any    `json:"warnings"`
	DiscoveryCode              *string             `json:"discoveryCode"`
	ErrorCode                  *string             `json:"errorCode"`
	FailureDetails             *FailureDetails     `json:"failureDetails"`
	RuntimeCheck               *RuntimeCheck       `json:"runtimeCheck"`
	Retryable                  bool                `json:"retryable"`
	ReviewItemID               *string             `json:"reviewItemId"`
	PublishedGameID            *string             `json:"publishedGameId"`
	ExistingGameID             *string             `json:"existingGameId"`
	ExistingMatches            []ExistingMatch     `json:"existingMatches"`
	UpdatedAtMS                int64               `json:"updatedAtMs"`
	Tags                       []tagging.Reference `json:"tags"`
}

type FailureDetails struct {
	SchemaVersion       int64   `json:"schemaVersion"`
	Stage               string  `json:"stage"`
	Operation           string  `json:"operation"`
	CauseCode           string  `json:"causeCode"`
	TechnicalDetail     string  `json:"technicalDetail"`
	RelativePath        *string `json:"relativePath"`
	ObservedFileCount   *int64  `json:"observedFileCount"`
	AllowedFileCount    *int64  `json:"allowedFileCount"`
	LibraryImportJobID  *string `json:"libraryImportJobId"`
	LibraryImportItemID *string `json:"libraryImportItemId"`
}

type RuntimeCheck struct {
	Status            string               `json:"status"`
	Code              string               `json:"code"`
	CoreID            string               `json:"coreId"`
	CoreName          string               `json:"coreName"`
	Machine           *string              `json:"machine"`
	MissingEntries    []string             `json:"missingEntries"`
	MismatchedEntries []string             `json:"mismatchedEntries"`
	Dependencies      []RuntimeDependency  `json:"dependencies"`
	BIOS              []RuntimeBIOS        `json:"bios"`
	MissingDiscs      []RuntimeMissingDisc `json:"missingDiscs"`
}

type RuntimeDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy"`
	ExpectedLogicalName string   `json:"expectedLogicalName"`
	State               string   `json:"state"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type RuntimeBIOS struct {
	LogicalName        string  `json:"logicalName"`
	RequirementMode    string  `json:"requirementMode"`
	ConditionCode      *string `json:"conditionCode"`
	InstallationStatus *string `json:"installationStatus"`
}

type RuntimeMissingDisc struct {
	Ordinal         int64  `json:"ordinal"`
	SourceReference string `json:"sourceReference"`
}

type ItemMedia struct {
	Cover string `json:"cover"`
	Video string `json:"video"`
}

type ExistingMatch struct {
	GameID string `json:"gameId"`
}
