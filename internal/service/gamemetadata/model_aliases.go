package gamemetadata

import model "retrom/internal/model/gamemetadata"

type (
	ApplyCandidateRequest    = model.ApplyCandidateRequest
	ApplyCandidateResult     = model.ApplyCandidateResult
	CandidateApplyRepository = model.CandidateApplyRepository
	CandidateApplyScope      = model.CandidateApplyScope
	CandidateApplySnapshot   = model.CandidateApplySnapshot
	CandidateAssetSelection  = model.CandidateAssetSelection
	GameMetadataUpdate       = model.GameMetadataUpdate
	Metadata                 = model.Metadata
	SelectedAssets           = model.SelectedAssets
)

var (
	ErrCandidateAsset    = model.ErrCandidateAsset
	ErrCandidateMetadata = model.ErrCandidateMetadata
	ErrCandidateStale    = model.ErrCandidateStale
	ErrInvalid           = model.ErrInvalid
	ErrMetadataInvalid   = model.ErrMetadataInvalid
	ErrVersionConflict   = model.ErrVersionConflict
)
