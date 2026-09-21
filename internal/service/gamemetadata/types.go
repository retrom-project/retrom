package gamemetadata

import (
	"context"
	"errors"
	"time"
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

// CandidateApplyScope is a transaction-bound persistence port. The service
// coordinates validation and ordering while persistence supplies the concrete
// transaction and SQL implementation.
type CandidateApplyScope interface {
	Load(context.Context, string, string) (CandidateApplySnapshot, error)
	ReplaceGameAssets(context.Context, string, string) ([]string, error)
	CreateSelectedGameAssets(context.Context, string, string, []CandidateAssetSelection, int64) ([]string, error)
	UpdateGameMetadata(context.Context, GameMetadataUpdate) (bool, error)
	StageCandidates(context.Context, []string) error
}

type CandidateApplyRepository interface {
	WithCandidateApply(context.Context, func(CandidateApplyScope) error) error
}

type Service struct {
	repository CandidateApplyRepository
	now        func() time.Time
}

func New(repository CandidateApplyRepository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, now: now}
}
