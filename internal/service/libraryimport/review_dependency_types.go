package libraryimport

import (
	"context"
	"encoding/json"
)

type ReviewDependencyHead struct {
	SnapshotID, ContentKind, PlatformID                 string
	ValidationStatus, CompatibilityCode, DependencyJSON *string
}
type ReviewDependencyReader interface {
	ArcadeRelationReader
	ArcadeAttachments(context.Context, string) ([]ArcadeAttachment, error)
	MultiDiscSource(context.Context, string) (MultiDiscSource, error)
	MultiDiscAttachments(context.Context, string) ([]MultiDiscAttachment, error)
}
type ArcadeAttachment struct {
	ID                  string          `json:"attachmentId"`
	Machine             string          `json:"machine"`
	ExpectedLogicalName string          `json:"expectedLogicalName"`
	OriginalFilename    string          `json:"originalFilename"`
	State               string          `json:"state"`
	ErrorCode           *string         `json:"errorCode"`
	JobID               string          `json:"jobId"`
	ObservedSizeBytes   *int64          `json:"observedSizeBytes"`
	ObservedSHA256      *string         `json:"observedSha256"`
	Diagnostics         json.RawMessage `json:"diagnostics"`
	CreatedAtMS         int64           `json:"createdAtMs"`
	UpdatedAtMS         int64           `json:"updatedAtMs"`
	FinishedAtMS        *int64          `json:"finishedAtMs"`
}
type ReviewArcadeNode struct {
	Kind                string            `json:"kind"`
	Machine             string            `json:"machine"`
	RequiredBy          *string           `json:"requiredBy"`
	Depth               int               `json:"depth"`
	ExpectedLogicalName string            `json:"expectedLogicalName"`
	State               string            `json:"state"`
	RequiredEntryCount  int               `json:"requiredEntryCount"`
	RequiredEntries     []string          `json:"requiredEntries"`
	CanAttach           bool              `json:"canAttach"`
	Attachment          *ArcadeAttachment `json:"attachment"`
	ManagementURL       string            `json:"managementUrl,omitempty"`
}
type ReviewArcade struct {
	Machine           string             `json:"machine"`
	Status            string             `json:"status"`
	CompatibilityCode string             `json:"compatibilityCode"`
	Nodes             []ReviewArcadeNode `json:"nodes"`
	ActiveAttachment  *ArcadeAttachment  `json:"activeAttachment"`
}
type MultiDiscPlaylist struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}
type MultiDiscEntry struct {
	Index           int     `json:"index"`
	DiscIndex       int     `json:"discIndex"`
	Label           string  `json:"label"`
	SourceReference string  `json:"sourceReference"`
	CanonicalName   string  `json:"canonicalName"`
	State           string  `json:"state"`
	LogicalName     *string `json:"logicalName"`
	SizeBytes       *int64  `json:"sizeBytes"`
	SHA256          *string `json:"sha256"`
}
type MultiDiscSource struct {
	Playlist      MultiDiscPlaylist
	Entries       []MultiDiscEntry
	MaxDiscs      int
	MaxTotalBytes int64
}
type MultiDiscAttachment struct {
	ID             string          `json:"attachmentId"`
	State          string          `json:"state"`
	ErrorCode      *string         `json:"errorCode"`
	Diagnostics    json.RawMessage `json:"diagnostics"`
	JobID          string          `json:"jobId"`
	JobState       string          `json:"jobState"`
	Version        int64           `json:"version"`
	JobVersion     int64           `json:"jobVersion"`
	CanRetry       bool            `json:"canRetry"`
	ErrorRetryable *bool           `json:"-"`
	CreatedAtMS    int64           `json:"createdAtMs"`
	UpdatedAtMS    int64           `json:"updatedAtMs"`
	FinishedAtMS   *int64          `json:"finishedAtMs"`
}
type ReviewMultiDisc struct {
	ContentKind           string               `json:"contentKind"`
	Playlist              MultiDiscPlaylist    `json:"playlist"`
	DiscCount             int                  `json:"discCount"`
	PresentDiscCount      int                  `json:"presentDiscCount"`
	MissingDiscCount      int                  `json:"missingDiscCount"`
	TotalPresentBytes     int64                `json:"totalPresentBytes"`
	MaxDiscs              int                  `json:"maxDiscs"`
	MaxTotalBytes         int64                `json:"maxTotalBytes"`
	Entries               []MultiDiscEntry     `json:"entries"`
	MissingReferences     []string             `json:"missingReferences"`
	LatestAttachment      *MultiDiscAttachment `json:"latestAttachment"`
	ActiveAttachment      *MultiDiscAttachment `json:"activeAttachment"`
	CanAttachMissingDiscs bool                 `json:"canAttachMissingDiscs"`
}
