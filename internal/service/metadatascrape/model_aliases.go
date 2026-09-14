package metadatascrape

import model "retrom/internal/model/metadatascrape"

type (
	ArcadeEvidence         = model.ArcadeEvidence
	AssetBlobs             = model.AssetBlobs
	AssetPublication       = model.AssetPublication
	AttemptRecord          = model.AttemptRecord
	CacheReader            = model.CacheReader
	CachedResponse         = model.CachedResponse
	CandidateAsset         = model.CandidateAsset
	CandidateAssetView     = model.CandidateAssetView
	CandidateHit           = model.CandidateHit
	CandidateIdentity      = model.CandidateIdentity
	CandidateRecord        = model.CandidateRecord
	DATBinding             = model.DATBinding
	EvidenceLookup         = model.EvidenceLookup
	EvidenceProgress       = model.EvidenceProgress
	EvidenceReader         = model.EvidenceReader
	EvidenceResults        = model.EvidenceResults
	WorkerEvidence         = model.WorkerEvidence
	FileEvidence           = model.FileEvidence
	GameSubject            = model.GameSubject
	HashEvidence           = model.HashEvidence
	Hashes                 = model.Hashes
	ImportSubject          = model.ImportSubject
	InitialAsset           = model.InitialAsset
	InitialCandidate       = model.InitialCandidate
	InitialDraft           = model.InitialDraft
	InitialDraftChange     = model.InitialDraftChange
	InitialImport          = model.InitialImport
	InitialProgressChange  = model.InitialProgressChange
	InitialReader          = model.InitialReader
	InitialReviewScope     = model.InitialReviewScope
	InitialWriter          = model.InitialWriter
	LookupAttempt          = model.LookupAttempt
	MediaInput             = model.MediaInput
	MediaInputEnvelope     = model.MediaInputEnvelope
	MediaInputScope        = model.MediaInputScope
	MediaAsset             = model.MediaAsset
	MediaAssets            = model.MediaAssets
	MediaClaim             = model.MediaClaim
	MediaJob               = model.MediaJob
	MediaLeases            = model.MediaLeases
	MediaOrder             = model.MediaOrder
	MediaJobPlan           = model.MediaJobPlan
	MediaQueueWriter       = model.MediaQueueWriter
	MediaOutcome           = model.MediaOutcome
	MediaProvider          = model.MediaProvider
	MediaReader            = model.MediaReader
	MediaRepository        = model.MediaRepository
	MediaScope             = model.MediaScope
	MediaSnapshot          = model.MediaSnapshot
	ResponseRecord         = model.ResponseRecord
	ResultReader           = model.ResultReader
	ResultRepository       = model.ResultRepository
	ResultScope            = model.ResultScope
	ResultWriter           = model.ResultWriter
	ResolvedLookup         = model.ResolvedLookup
	ReviewCandidate        = model.ReviewCandidate
	ReviewCandidateRecord  = model.ReviewCandidateRecord
	ReviewChange           = model.ReviewChange
	ReviewEvidence         = model.ReviewEvidence
	ReviewEvidenceReader   = model.ReviewEvidenceReader
	ReviewRun              = model.ReviewRun
	ReviewRunOutcomes      = model.ReviewRunOutcomes
	ReviewSubject          = model.ReviewSubject
	ScheduleEvidenceReader = model.ScheduleEvidenceReader
	SchedulePlan           = model.SchedulePlan
	ScheduleReader         = model.ScheduleReader
	ScheduleRepository     = model.ScheduleRepository
	ScheduleScope          = model.ScheduleScope
	ScheduleWriter         = model.ScheduleWriter
	ScrapeDispatcher       = model.ScrapeDispatcher
	Subject                = model.Subject
	WorkerClaim            = model.WorkerClaim
	WorkerLeases           = model.WorkerLeases
	WorkerOutcome          = model.WorkerOutcome
	WorkerProcessor        = model.WorkerProcessor
	WorkerRepository       = model.WorkerRepository
	WorkerRun              = model.WorkerRun
	WorkerScope            = model.WorkerScope
	WorkerStatus           = model.WorkerStatus
	WorkerWriter           = model.WorkerWriter
)

const MediaRunBudget = model.MediaRunBudget

var (
	ErrAssetStateConflict    = model.ErrAssetStateConflict
	ErrArcadeSnapshotInvalid = model.ErrArcadeSnapshotInvalid
	ErrAttemptsExhausted     = model.ErrAttemptsExhausted
	ErrExecutionLost         = model.ErrExecutionLost
	ErrGameVersionConflict   = model.ErrGameVersionConflict
	ErrGameDeleted           = model.ErrGameDeleted
	ErrInitialItemState      = model.ErrInitialItemState
	ErrInitialProgressState  = model.ErrInitialProgressState
	ErrMediaBudget           = model.ErrMediaBudget
	ErrMediaInput            = model.ErrMediaInput
	ErrProviderInvalid       = model.ErrProviderInvalid
	ErrReviewVersionConflict = model.ErrReviewVersionConflict
)
