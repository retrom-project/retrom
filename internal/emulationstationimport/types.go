package emulationstationimport

import "retrom/internal/tagging"

type Root struct {
	ID, Label string
	path      string
	digest    string
}

type CreateRequest struct {
	RootID             string `json:"rootId"`
	SourceRelativePath string `json:"sourceRelativePath"`
}

type RootRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CreatedBy struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Counts struct {
	Gamelists            int64 `json:"gamelists"`
	InvalidGamelists     int64 `json:"invalidGamelists"`
	Collections          int64 `json:"collections"`
	FoldersIgnored       int64 `json:"foldersIgnored"`
	Games                int64 `json:"games"`
	EstimatedSourceBytes int64 `json:"estimatedSourceBytes"`
	MappedCollections    int64 `json:"mappedCollections"`
	SkippedCollections   int64 `json:"skippedCollections"`
	SkippedMapping       int64 `json:"skippedMapping"`
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
	GamelistRelativePath       string              `json:"gamelistRelativePath"`
	RelativeDirectory          string              `json:"relativeDirectory"`
	DisplayName                string              `json:"displayName"`
	GameCount                  int64               `json:"gameCount"`
	IssueCount                 int64               `json:"issueCount"`
	FolderEntryCount           int64               `json:"folderEntryCount"`
	HiddenGameCount            int64               `json:"hiddenGameCount"`
	AdultGameCount             int64               `json:"adultGameCount"`
	ExtensionSummary           []ExtensionSummary  `json:"extensionSummary"`
	ExtensionOtherCount        int64               `json:"extensionOtherCount"`
	MappingAction              *string             `json:"mappingAction"`
	TargetPlatformInstanceID   *string             `json:"targetPlatformInstanceId"`
	TargetPlatformInstanceName *string             `json:"targetPlatformInstanceName"`
	TargetDefaultCoreID        *string             `json:"targetDefaultCoreId"`
	TargetDefaultCoreName      *string             `json:"targetDefaultCoreName"`
	TagSnapshot                []tagging.Reference `json:"tagSnapshot"`
}

type ExtensionSummary struct {
	Extension string `json:"extension"`
	Count     int64  `json:"count"`
}

type Gamelist struct {
	RelativePath           string   `json:"relativePath"`
	ParseState             string   `json:"parseState"`
	ErrorCode              *string  `json:"errorCode"`
	GameCount              int64    `json:"gameCount"`
	FolderCount            int64    `json:"folderCount"`
	ProviderPresent        bool     `json:"providerPresent"`
	IgnoredFieldNames      []string `json:"ignoredFieldNames"`
	IgnoredFieldOtherCount int64    `json:"ignoredFieldOtherCount"`
	CreatedAtMS            int64    `json:"createdAtMs"`
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
	GamelistRelativePath       string              `json:"gamelistRelativePath"`
	SourceFlags                SourceFlags         `json:"sourceFlags"`
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

type SourceFlags struct {
	Hidden  bool `json:"hidden"`
	Adult   bool `json:"adult"`
	KidGame bool `json:"kidGame"`
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
