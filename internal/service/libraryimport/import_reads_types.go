package libraryimport

import (
	"context"
	"encoding/json"
)

// ImportOverviewSummary is the aggregate displayed on the administrator import
// dashboard.  It intentionally combines ordinary and source specific imports
// into the same projection used by the HTTP API.
type ImportOverviewSummary struct {
	Running                int64 `json:"running"`
	ReviewPending          int64 `json:"reviewPending"`
	PublishedItems         int64 `json:"publishedItems"`
	Completed              int64 `json:"completed"`
	Failed                 int64 `json:"failed"`
	OrdinaryFailed         int64 `json:"ordinaryFailed"`
	PegasusFailed          int64 `json:"pegasusFailed"`
	EmulationStationFailed int64 `json:"emulationStationFailed"`
	ProcessingItems        int64 `json:"processingItems"`
	IssueItems             int64 `json:"issueItems"`
}

// ImportListQuery is the storage independent input for the administrator
// import list.  Cursor decoding remains at the HTTP boundary; persistence only
// receives the decoded keyset values.
type ImportListQuery struct {
	QueryText   string
	State       string
	PlatformID  string
	SortCode    string
	CursorID    string
	CursorValue int64
	Limit       int
}

type ImportListItem struct {
	ID                          string  `json:"id"`
	State                       string  `json:"state"`
	PlatformInstanceName        string  `json:"platformInstanceName"`
	MetadataProvider            string  `json:"metadataProvider"`
	ContentMode                 string  `json:"contentMode"`
	TotalItemCount              int64   `json:"totalItemCount"`
	ReviewPendingItemCount      int64   `json:"reviewPendingItemCount"`
	FailedItemCount             int64   `json:"failedItemCount"`
	RejectedFileCount           int64   `json:"rejectedFileCount"`
	UnresolvedRejectedFileCount int64   `json:"unresolvedRejectedFileCount"`
	AlreadyImportedItemCount    int64   `json:"alreadyImportedItemCount"`
	AlreadyImportedFileCount    int64   `json:"alreadyImportedFileCount"`
	LastErrorCode               *string `json:"lastErrorCode"`
	Version                     int64   `json:"version"`
	CreatedAtMS                 int64   `json:"createdAtMs"`
	UpdatedAtMS                 int64   `json:"updatedAtMs"`
}

type ImportDetailTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ImportDetailCounts struct {
	Total                   int64 `json:"total"`
	Queued                  int64 `json:"queued"`
	Running                 int64 `json:"running"`
	ReviewPending           int64 `json:"reviewPending"`
	Published               int64 `json:"published"`
	Discarded               int64 `json:"discarded"`
	Failed                  int64 `json:"failed"`
	Cancelled               int64 `json:"cancelled"`
	IgnoredFiles            int64 `json:"ignoredFiles"`
	RejectedFiles           int64 `json:"rejectedFiles"`
	UnresolvedRejectedFiles int64 `json:"unresolvedRejectedFiles"`
	AlreadyImportedItems    int64 `json:"alreadyImportedItems"`
	AlreadyImportedFiles    int64 `json:"alreadyImportedFiles"`
}

type ImportFileResolution struct {
	Action                 string `json:"action"`
	ReplacementImportJobID string `json:"replacementImportJobId"`
	ResolvedAtMS           int64  `json:"resolvedAtMs"`
}

type ImportFileOutcome struct {
	UploadFileID string                `json:"uploadFileId"`
	Name         string                `json:"name"`
	SizeBytes    int64                 `json:"sizeBytes"`
	Disposition  string                `json:"disposition"`
	ReasonCode   *string               `json:"reasonCode"`
	Resolution   *ImportFileResolution `json:"resolution"`
}

type ImportDuplicateMatch struct {
	ImportItemID          string `json:"importItemId"`
	ContentIdentityDigest string `json:"contentIdentityDigest"`
	ExistingGame          struct {
		ID                   string `json:"id"`
		Title                string `json:"title"`
		PlatformInstanceID   string `json:"platformInstanceId"`
		PlatformInstanceName string `json:"platformInstanceName"`
	} `json:"existingGame"`
}

type ImportMultiDiscItemSummary struct {
	ItemID           string   `json:"itemId"`
	State            string   `json:"state"`
	ContentKind      string   `json:"contentKind"`
	Playlist         string   `json:"playlist"`
	DiscCount        int64    `json:"discCount"`
	PresentDiscCount int64    `json:"presentDiscCount"`
	MissingDiscCount int64    `json:"missingDiscCount"`
	IgnoredFileCount int      `json:"ignoredFileCount"`
	IgnoredFiles     []string `json:"ignoredFiles"`
	PlaylistPath     string   `json:"-"`
}

type ImportDetail struct {
	ImportJobID                 string                       `json:"importJobId"`
	UploadID                    string                       `json:"uploadId"`
	TargetPlatformInstance      ImportDetailTarget           `json:"targetPlatformInstance"`
	PlatformID                  string                       `json:"platformId"`
	DefaultCoreID               string                       `json:"defaultCoreId"`
	ProviderID                  string                       `json:"providerId"`
	TargetID                    string                       `json:"targetId"`
	DatVersionID                *string                      `json:"datVersionId"`
	MetadataProvider            string                       `json:"metadataProvider"`
	ReconfiguredFromImportJobID *string                      `json:"reconfiguredFromImportJobId"`
	ConfigSnapshot              any                          `json:"configSnapshot"`
	FileOutcomes                []ImportFileOutcome          `json:"fileOutcomes"`
	AlreadyImportedMatches      []ImportDuplicateMatch       `json:"alreadyImportedMatches"`
	ItemSummaries               []ImportMultiDiscItemSummary `json:"itemSummaries"`
	State                       string                       `json:"state"`
	PayloadState                string                       `json:"payloadState"`
	PayloadReleaseJobID         *string                      `json:"payloadReleaseJobId"`
	Counts                      ImportDetailCounts           `json:"counts"`
	ErrorCode                   *string                      `json:"errorCode"`
	CancelReason                *string                      `json:"cancelReason"`
	Version                     int64                        `json:"version"`
	CreatedAtMS                 int64                        `json:"createdAtMs"`
	UpdatedAtMS                 int64                        `json:"updatedAtMs"`
}

type ReviewHistoryQuery struct {
	QueryText string
	Decision  string
}

type ReviewHistoryItem struct {
	ReviewEventID string  `json:"reviewEventId"`
	ImportItemID  string  `json:"importItemId"`
	ImportJobID   string  `json:"importJobId"`
	Title         string  `json:"title"`
	Decision      string  `json:"decision"`
	Reason        *string `json:"reason"`
	CreatedAtMS   int64   `json:"createdAtMs"`
}

type ReviewHistoryActor struct {
	Kind   string  `json:"kind"`
	UserID *string `json:"userId"`
	Label  *string `json:"label"`
}

type ReviewHistoryEvent struct {
	ReviewEventID    string             `json:"reviewEventId"`
	ImportItemID     string             `json:"importItemId"`
	EventType        string             `json:"eventType"`
	Actor            ReviewHistoryActor `json:"actor"`
	Before           any                `json:"before"`
	After            any                `json:"after"`
	Diff             any                `json:"diff"`
	ConfigEvidence   any                `json:"configEvidence"`
	DANEvidence      any                `json:"datEvidence"`
	ProviderEvidence any                `json:"providerEvidence"`
	Reason           *string            `json:"reason"`
	CreatedAtMS      int64              `json:"createdAtMs"`
}

// ImportReadRepository owns all SQL projections used by the administrator
// import and review history endpoints.
//
//nolint:interfacebloat // these projections are one cohesive administrator read boundary
type ImportReadRepository interface {
	Summary(context.Context) (ImportOverviewSummary, error)
	List(context.Context, ImportListQuery) ([]ImportListItem, error)
	Detail(context.Context, string) (ImportDetail, error)
	MultiDiscItemSummaries(context.Context, string) ([]ImportMultiDiscItemSummary, error)
	ReviewHistory(context.Context, ReviewHistoryQuery) ([]ReviewHistoryItem, error)
	ReviewHistoryEvent(context.Context, string) (ReviewHistoryEvent, error)
}

// ImportReadDocuments is kept separate from the SQL row types so the service
// owns the tolerant JSON projection behavior exposed by the old handlers.
func DecodeImportDocument(value string) any {
	var result any
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}
