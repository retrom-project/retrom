package gamemove

import model "retrom/internal/model/gamemove"

type (
	AuditActor             = model.AuditActor
	AuditEvent             = model.AuditEvent
	CandidateRecord        = model.CandidateRecord
	Impact                 = model.Impact
	ImpactSubject          = model.ImpactSubject
	MoveRequest            = model.MoveRequest
	MoveResult             = model.MoveResult
	MoveScope              = model.MoveScope
	PreviewRequest         = model.PreviewRequest
	Repository             = model.Repository
	ScrapeCandidatesResult = model.ScrapeCandidatesResult
	ValidationResolver     = model.ValidationResolver
	VariantQuery           = model.VariantQuery
	VariantState           = model.VariantState
)

var (
	ErrImpactStale           = model.ErrImpactStale
	ErrInvalid               = model.ErrInvalid
	ErrValidationUnavailable = model.ErrValidationUnavailable
	ErrVersionConflict       = model.ErrVersionConflict
)
