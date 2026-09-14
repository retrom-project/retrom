package platforminstance

import model "retrom/internal/model/platforminstance"

const applyOperationID = "postAdminPlatformInstanceRecommendationsApply"

type (
	ApplyResult             = model.ApplyResult
	ApplySummary            = model.ApplySummary
	AuditActor              = model.AuditActor
	AuditEvent              = model.AuditEvent
	CatalogReference        = model.CatalogReference
	CoreImpact              = model.CoreImpact
	CoreImpactFacts         = model.CoreImpactFacts
	CoreImpactGame          = model.CoreImpactGame
	CoreImpactItem          = model.CoreImpactItem
	CoreImpactResult        = model.CoreImpactResult
	CreateInput             = model.CreateInput
	CreationAudit           = model.CreationAudit
	DefaultCoreChange       = model.DefaultCoreChange
	DefaultCoreChangeResult = model.DefaultCoreChangeResult
	Directory               = model.Directory
	DirectoryDelete         = model.DirectoryDelete
	DirectoryUpdate         = model.DirectoryUpdate
	DirectoryWrites         = model.DirectoryWrites
	IdempotencyKey          = model.IdempotencyKey
	IdempotencyRecord       = model.IdempotencyRecord
	IdempotencyRecords      = model.IdempotencyRecords
	IdempotentResponse      = model.IdempotentResponse
	Instance                = model.Instance
	NewDirectory            = model.NewDirectory
	Reader                  = model.Reader
	Recommendation          = model.Recommendation
	RecommendationSummary   = model.RecommendationSummary
	Recommendations         = model.Recommendations
	Reference               = model.Reference
	Repository              = model.Repository
	State                   = model.State
	WriteScope              = model.WriteScope
)

const (
	StateActive              = model.StateActive
	StateCoveredByEquivalent = model.StateCoveredByEquivalent
	StateCustomized          = model.StateCustomized
	StateMissing             = model.StateMissing
	StateSuppressed          = model.StateSuppressed
)

var (
	ErrCatalogInvalid     = model.ErrCatalogInvalid
	ErrDefaultCoreBlocked = model.ErrDefaultCoreBlocked
	ErrDefaultCoreInvalid = model.ErrDefaultCoreInvalid
	ErrIdempotencyReused  = model.ErrIdempotencyReused
	ErrImpactStale        = model.ErrImpactStale
	ErrInvalid            = model.ErrInvalid
	ErrInvalidCore        = model.ErrInvalidCore
	ErrNotEmpty           = model.ErrNotEmpty
	ErrNotFound           = model.ErrNotFound
	ErrOrderStale         = model.ErrOrderStale
	ErrSlugExhausted      = model.ErrSlugExhausted
	ErrVersionConflict    = model.ErrVersionConflict
)
