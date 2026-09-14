package serverimport

import model "retrom/internal/model/serverimport"

type (
	AutomaticRetry      = model.AutomaticRetry
	Cancellation        = model.Cancellation
	Candidate           = model.Candidate
	CandidateCursor     = model.CandidateCursor
	CandidateEvidence   = model.CandidateEvidence
	CandidateQuery      = model.CandidateQuery
	CandidateWrite      = model.CandidateWrite
	CatalogEntry        = model.CatalogEntry
	CatalogItem         = model.CatalogItem
	ControlEvidence     = model.ControlEvidence
	ControlReader       = model.ControlReader
	ControlRepository   = model.ControlRepository
	ControlScope        = model.ControlScope
	ControlSnapshot     = model.ControlSnapshot
	ControlWriter       = model.ControlWriter
	Counts              = model.Counts
	CreateRequest       = model.CreateRequest
	CreatedBy           = model.CreatedBy
	CreationPlan        = model.CreationPlan
	CreationRepository  = model.CreationRepository
	CreationWriter      = model.CreationWriter
	DiscoveryGroup      = model.DiscoveryGroup
	DiscoveryPlan       = model.DiscoveryPlan
	DiscoveryRecords    = model.DiscoveryRecords
	DiscoveryRepository = model.DiscoveryRepository
	FinalOutcome        = model.FinalOutcome
	Item                = model.Item
	ItemCursor          = model.ItemCursor
	ItemQuery           = model.ItemQuery
	ItemOutcome         = model.ItemOutcome
	LeaseClaim          = model.LeaseClaim
	LeaseRecords        = model.LeaseRecords
	LeaseRepository     = model.LeaseRepository
	LeaseSnapshot       = model.LeaseSnapshot
	LeaseTouch          = model.LeaseTouch
	ListQuery           = model.ListQuery
	ManualRetry         = model.ManualRetry
	OutcomeReader       = model.OutcomeReader
	OutcomeRepository   = model.OutcomeRepository
	OutcomeScope        = model.OutcomeScope
	OutcomeWriter       = model.OutcomeWriter
	RecoveryWork        = model.RecoveryWork
	RetryBudget         = model.RetryBudget
	RootRef             = model.RootRef
	RootSelection       = model.RootSelection
	SourceSelector      = model.SourceSelector
	Summary             = model.Summary
	SummaryCursor       = model.SummaryCursor
	TerminalCounts      = model.TerminalCounts
	Work                = model.Work
	WorkerAccess        = model.WorkerAccess
)

const (
	CancelledWorker = model.CancelledWorker
	ExhaustedWorker = model.ExhaustedWorker
	RunningWorker   = model.RunningWorker
)

var (
	ErrActive          = model.ErrActive
	ErrCatalogEmpty    = model.ErrCatalogEmpty
	ErrCatalogInvalid  = model.ErrCatalogInvalid
	ErrLeaseLost       = model.ErrLeaseLost
	ErrNotCancellable  = model.ErrNotCancellable
	ErrNotFound        = model.ErrNotFound
	ErrNotRetryable    = model.ErrNotRetryable
	ErrQuery           = model.ErrQuery
	ErrWorkerCancelled = model.ErrWorkerCancelled
)

var CanonicalCatalogJSON = model.CanonicalCatalogJSON
