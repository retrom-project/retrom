package gamemetadata

import (
	"context"
	"errors"
)

var (
	ErrInvalid           = errors.New("GAME_METADATA_INVALID")
	ErrCandidateStale    = errors.New("GAME_METADATA_CANDIDATE_STALE")
	ErrCandidateMetadata = errors.New("GAME_METADATA_CANDIDATE_METADATA_INVALID")
	ErrMetadataInvalid   = errors.New("GAME_METADATA_VALUE_INVALID")
	ErrCandidateAsset    = errors.New("GAME_METADATA_CANDIDATE_ASSET_INVALID")
	ErrVersionConflict   = errors.New("GAME_METADATA_VERSION_CONFLICT")
)

// Metadata is the editable game metadata snapshot used by the candidate apply
// workflow. Pointers preserve nullable players and release year values without
// leaking SQL null types into the application layer.
type Metadata struct {
	Title       string
	Description string
	Developer   string
	Publisher   string
	Genre       string
	Players     *int64
	ReleaseYear *int64
}

type SelectedAssets struct {
	CoverCandidateAssetID       *string
	BackgroundCandidateAssetID  *string
	ScreenshotCandidateAssetIDs []string
}

type ApplyCandidateRequest struct {
	GameID          string
	CandidateID     string
	ExpectedVersion int64
	Fields          []string
	SelectedAssets  SelectedAssets
}

type ApplyCandidateResult struct {
	AssetIDs        []string
	ReplacedBlobIDs []string
	Version         int64
	UpdatedAtMS     int64
}

type CandidateApplySnapshot struct {
	Version               int64
	Current               Metadata
	CandidateMetadataJSON string
}

type CandidateAssetSelection struct {
	ID      string
	Kind    string
	Ordinal int64
}

type GameMetadataUpdate struct {
	GameID          string
	CandidateID     string
	ExpectedVersion int64
	Metadata        Metadata
	NowMS           int64
}

type CandidateApplyRepository interface {
	LoadCandidateApplySnapshot(context.Context, string, string) (CandidateApplySnapshot, error)
	CommitCandidateApply(context.Context, CandidateApplyCommand) (CandidateApplyCommitResult, error)
}

// CandidateApplyCommand carries the values for an atomic candidate apply write.
type CandidateApplyCommand struct {
	GameID, CandidateID string
	ExpectedVersion     int64
	NowMS               int64
	Metadata            Metadata
	SelectedAssets      []CandidateAssetSelection
	SelectedKinds       []string
}

// CandidateApplyCommitResult carries the write-side outputs.
type CandidateApplyCommitResult struct {
	ReplacedBlobIDs []string
	AssetIDs        []string
}
