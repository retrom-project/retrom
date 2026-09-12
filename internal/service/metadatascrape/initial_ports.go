package metadatascrape

import (
	"context"
	"errors"
)

var (
	ErrInitialItemState     = errors.New("initial review item state changed")
	ErrInitialProgressState = errors.New("initial import progress state changed")
)

type InitialImport struct {
	ItemID, ImportJobID, ItemState     string
	Running, Failed, Rejected, Version int64
}
type InitialCandidate struct {
	ID, MetadataJSON, ProviderGameID string
	HitCount, FirstQueryOrder        int64
}
type (
	InitialDraft struct{ ID, MetadataJSON string }
	InitialAsset struct {
		ID, Kind string
		Ordinal  int
	}
)

type InitialReader interface {
	Import(context.Context, string) (InitialImport, bool, error)
	Candidates(context.Context, string) ([]InitialCandidate, error)
	Draft(context.Context, string) (InitialDraft, error)
	ReadyAssets(context.Context, string) ([]InitialAsset, error)
}
type InitialDraftChange struct {
	ItemID, DraftID, CandidateID, MetadataJSON, Title string
	CoverID, BackgroundID                             *string
	Screenshots                                       []InitialAsset
	Now                                               int64
}
type InitialProgressChange struct {
	ItemID, ImportJobID, ItemState, JobState                        string
	FailedStage, ErrorCode                                          *string
	ExpectedRunning, ExpectedVersion, ReviewDelta, FailedDelta, Now int64
}
type InitialWriter interface {
	Apply(context.Context, InitialDraftChange) error
	Advance(context.Context, InitialProgressChange) error
}
type InitialReviewScope struct {
	Read  InitialReader
	Write InitialWriter
}
