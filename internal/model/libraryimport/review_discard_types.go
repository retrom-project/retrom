package libraryimport

import (
	"context"

	"retrom/internal/model/payloadrelease"

	"retrom/internal/model/importprogress"
	"retrom/internal/model/tagging"
)

type ReviewDiscardMode string

const (
	ReviewDiscardSingle ReviewDiscardMode = "SINGLE"
	ReviewDiscardBatch  ReviewDiscardMode = "BATCH"
)

type ReviewOwnerState string

const (
	ReviewOwnerPublished ReviewOwnerState = "PUBLISHED"
	ReviewOwnerDiscarded ReviewOwnerState = "REVIEW_DISCARDED"
)

type ReviewDiscardRequest struct {
	ItemID, Reason  string
	ExpectedVersion int64
	Mode            ReviewDiscardMode
}
type ReviewDecisionResult struct {
	ItemID      string `json:"itemId"`
	EventID     string `json:"reviewEventId"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}
type ReviewDiscardSnapshot struct {
	DraftID, ImportID, MetadataJSON   string
	Version                           int64
	State, HandoffKind                string
	EmulationStationReady, SourceBusy bool
	ValidationID, DatID, CandidateID  *string
	HasCover, HasBackground           bool
	Aggregate                         ReviewDiscardAggregate
}
type ReviewDiscardAggregate struct {
	Version  int64
	Progress importprogress.Snapshot
}
type ReviewDiscardAggregateChange struct {
	ExpectedVersion, ExpectedPending int64
	Projection                       importprogress.Projection
}
type ReviewDiscardChange struct {
	ItemID, ImportID string
	ExpectedVersion  int64
	NowMS            int64
	Aggregate        ReviewDiscardAggregateChange
}
type ReviewDiscardEvent struct {
	ID, ItemID, Reason                            string
	ActorKind                                     string
	ActorUserID, ActorLabel                       *string
	BeforeJSON, ConfigJSON, DatJSON, ProviderJSON string
	NowMS                                         int64
}
type ReviewOwnerTransition struct {
	Mode   ReviewDiscardMode
	ItemID string
	State  ReviewOwnerState
	GameID *string
	NowMS  int64
}
type DiscardCommand struct {
	Request ReviewDiscardRequest
	EventID string
	Actor   ReviewActor
	NowMS   int64
}

type ReviewActor struct {
	Kind    string
	UserID  *string
	Label   *string
}

type ReviewDiscardRepository interface {
	WithDiscard(context.Context, func(ReviewDiscardScope) error) error
	CommitDiscard(context.Context, DiscardCommand) (ReviewDecisionResult, error)
}

type ReviewBatchItem struct {
	ItemID  string
	Version int64
}

type ReviewBatchDiscardRepository interface {
	Pending(context.Context, string, int) ([]ReviewBatchItem, error)
	Release(context.Context, string, int64) error
}
type ReviewDiscardScope struct {
	Payload payloadrelease.ReleaseScope
	Reader  ReviewDiscardReader
	Tags    tagging.ReferenceReader
	Writer  ReviewDiscardWriter
}
type ReviewDiscardReader interface {
	Snapshot(context.Context, string) (ReviewDiscardSnapshot, bool, error)
}
type ReviewDiscardWriter interface {
	CancelAttachments(context.Context, string, int64) error
	DiscardItem(context.Context, ReviewDiscardChange) error
	RecordEvent(context.Context, ReviewDiscardEvent) error
	TransitionOwner(context.Context, ReviewOwnerTransition) error
}
