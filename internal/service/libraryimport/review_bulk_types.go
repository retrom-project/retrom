package libraryimport

import (
	"context"
	"errors"
)

const ReviewBulkQueryLimit = 10_001

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
	ItemID, SourceSnapshotID, PlatformID string
	ReviewVersion                        int64
	ValidationStatus                     *string
	AttachmentActive, SourceFlagged      bool
}

type ReviewBulkSummary struct {
	BulkApprovalID        string  `json:"bulkApprovalId"`
	JobID                 string  `json:"jobId"`
	State                 string  `json:"state"`
	Version               int64   `json:"version"`
	MaxItemID             string  `json:"maxItemId"`
	CursorItemID          *string `json:"cursorItemId"`
	InitialPendingCount   int     `json:"initialPendingCount"`
	ScannedCount          int     `json:"scannedCount"`
	PublishedCount        int     `json:"publishedCount"`
	SkippedChangedCount   int     `json:"skippedChangedCount"`
	SkippedDuplicateCount int     `json:"skippedDuplicateCount"`
	SkippedNotReadyCount  int     `json:"skippedNotReadyCount"`
	CreatedAtMS           int64   `json:"createdAtMs"`
	UpdatedAtMS           int64   `json:"updatedAtMs"`
	StartedAtMS           *int64  `json:"startedAtMs"`
	CompletedAtMS         *int64  `json:"completedAtMs"`
	LastErrorCode         *string `json:"lastErrorCode"`
}

type ReviewBulkRepository interface {
	Candidates(context.Context, ReviewBulkCandidateQuery) ([]ReviewBulkCandidate, error)
}
