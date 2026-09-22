package libraryimport

import (
	"context"
	"encoding/json"
	"errors"

	"retrom/internal/contentcapability"
)

// ReviewBulkQueryLimit is the maximum number of candidate rows a preview can
// inspect in one read. A caller can use a smaller limit for bounded jobs such
// as deduplication.
const ReviewBulkQueryLimit = 10_001

const ReviewBulkItemLimit = 50

var ErrReviewBulkQuery = errors.New("INVALID_REVIEW_BULK_QUERY")

type ReviewBulkScope struct {
	Q                  string `json:"q,omitempty"`
	TagID              string `json:"tagId,omitempty"`
	ImportJobID        string `json:"importJobId,omitempty"`
	SourceImportID     string `json:"sourceImportId,omitempty"`
	PlatformInstanceID string `json:"platformInstanceId,omitempty"`
	BlockerCode        string `json:"blockerCode,omitempty"`
}

type ReviewBulkCandidateQuery struct {
	Scope         ReviewBulkScope
	AfterItemID   string
	ThroughItemID string
	Limit         int
}

type ReviewBulkCandidate struct {
	ItemID, SourceSnapshotID, PlatformInstanceID       string
	PlatformName, PlatformID, Title, ContentKind       string
	ReviewVersion, PlatformVersion                     int64
	ProviderID, TargetID                               *string
	ContentPolicy                                      contentcapability.Policy
	ValidationID, ValidationStatus                     *string
	ValidationPlatformVersion                          *int64
	ValidationDAT, CurrentDAT                          *string
	ValidationDOSEntry, DraftDOSEntry                  *string
	DependencySnapshot                                 *string
	ScreenshotCurrent, AttachmentActive, SourceFlagged bool
}

// ReviewBulkItemQuery uses an ordinal cursor so callers do not need to know
// about the storage representation of the page cursor.
type ReviewBulkItemQuery struct {
	BulkApprovalID string
	Outcome        string
	AfterOrdinal   int
	Limit          int
}

type ReviewBulkItemRecord struct {
	ImportItemID   string          `json:"importItemId"`
	Title          string          `json:"title"`
	PlatformName   string          `json:"platformName"`
	State          string          `json:"state"`
	GameID         *string         `json:"gameId"`
	OutcomeCode    *string         `json:"outcomeCode"`
	OutcomeDetails json.RawMessage `json:"outcomeDetails"`
	CompletedAtMS  *int64          `json:"completedAtMs"`
	Ordinal        int             `json:"-"`
}

type ReviewBulkItemPage struct {
	Items      []ReviewBulkItemRecord `json:"items"`
	NextCursor *string                `json:"nextCursor"`
}

type ReviewBulkCounts struct {
	Matched          int `json:"matched"`
	StrictReady      int `json:"strictReady"`
	ScreenshotOnly   int `json:"screenshotOnly"`
	Duplicate        int `json:"duplicate"`
	AttachmentActive int `json:"attachmentActive"`
	SourceFlagged    int `json:"sourceFlagged"`
	NotReadyOrStale  int `json:"notReadyOrStale"`
}

type ReviewBulkProgress struct {
	Candidate        int `json:"candidate"`
	Processed        int `json:"processed"`
	Published        int `json:"published"`
	SkippedDuplicate int `json:"skippedDuplicate"`
	SkippedChanged   int `json:"skippedChanged"`
	SkippedNotReady  int `json:"skippedNotReady"`
	Failed           int `json:"failed"`
	Cancelled        int `json:"cancelled"`
}

type ReviewBulkSummary struct {
	BulkApprovalID string             `json:"bulkApprovalId"`
	JobID          string             `json:"jobId"`
	State          string             `json:"state"`
	Version        int64              `json:"version"`
	Scope          ReviewBulkScope    `json:"scope"`
	Counts         ReviewBulkCounts   `json:"initialCounts"`
	Progress       ReviewBulkProgress `json:"counts"`
	CreatedAtMS    int64              `json:"createdAtMs"`
	UpdatedAtMS    int64              `json:"updatedAtMs"`
	StartedAtMS    *int64             `json:"startedAtMs"`
	CompletedAtMS  *int64             `json:"completedAtMs"`
	LastErrorCode  *string            `json:"lastErrorCode"`
}

type ReviewBulkCreation struct {
	BulkApprovalID          string
	JobID                   string
	CreatedByUserID         string
	Scope                   ReviewBulkScope
	ScopeJSON               string
	ScopeDigest             string
	CandidateManifestDigest string
	PayloadJSON             string
	DedupeKey               string
	InputDigest             string
	Counts                  ReviewBulkCounts
	Candidates              []ReviewBulkCandidate
	NowMS                   int64
}

type ReviewBulkRepository interface {
	Candidates(context.Context, ReviewBulkCandidateQuery) ([]ReviewBulkCandidate, error)
	Items(context.Context, ReviewBulkItemQuery) ([]ReviewBulkItemRecord, error)
	Summary(context.Context, string) (ReviewBulkSummary, error)
	ActiveSummary(context.Context) (ReviewBulkSummary, bool, error)
}
