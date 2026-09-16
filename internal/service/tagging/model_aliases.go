// model_aliases.go re-exports model types for callers that historically
// imported them from service/tagging. These aliases will be removed once all
// callers are migrated to import model/tagging directly (P4/P5/P6).
package tagging

import model "retrom/internal/model/tagging"

type (
	AdminItem              = model.AdminItem
	Assignment             = model.Assignment
	AuditEvent             = model.AuditEvent
	AuditRecords           = model.AuditRecords
	CommonTagsResult       = model.CommonTagsResult
	DeleteImpact           = model.DeleteImpact
	GameRecords            = model.GameRecords
	GameTagResult          = model.GameTagResult
	InvalidReferencesError = model.InvalidReferencesError
	ListFilter             = model.ListFilter
	ListQuery              = model.ListQuery
	Owner                  = model.Owner
	OwnerKind              = model.OwnerKind
	Reference              = model.Reference
	ReferenceReader        = model.ReferenceReader
	RelationRecords        = model.RelationRecords
	Repository             = model.Repository
	ReplacementPlan        = model.ReplacementPlan
	Summary                = model.Summary
	TagReader              = model.TagReader
	TagWrite               = model.TagWrite
	TagWriter              = model.TagWriter
	Usage                  = model.Usage
	WriteScope             = model.WriteScope
)

const (
	DefaultListLimit                = model.DefaultListLimit
	MaxActiveTags                   = model.MaxActiveTags
	MaxTagsPerOwner                 = model.MaxTagsPerOwner
	MaximumListLimit                = model.MaximumListLimit
	MaximumNameBytes                = model.MaximumNameBytes
	MaximumNameRunes                = model.MaximumNameRunes
	OwnerEmulationStationCollection = model.OwnerEmulationStationCollection
	OwnerGame                       = model.OwnerGame
	OwnerPegasusCollection          = model.OwnerPegasusCollection
	OwnerReviewDraft                = model.OwnerReviewDraft
	OwnerReviewItem                 = model.OwnerReviewItem
	SortNameAsc                     = model.SortNameAsc
	SortUpdatedDesc                 = model.SortUpdatedDesc
	StatusActive                    = model.StatusActive
	StatusDeleted                   = model.StatusDeleted
)

var (
	ErrAlreadyDeleted          = model.ErrAlreadyDeleted
	ErrAssignmentLimitExceeded = model.ErrAssignmentLimitExceeded
	ErrDeleteConfirmation      = model.ErrDeleteConfirmation
	ErrGameNotFound            = model.ErrGameNotFound
	ErrInvalid                 = model.ErrInvalid
	ErrLimitReached            = model.ErrLimitReached
	ErrNameConflict            = model.ErrNameConflict
	ErrNameInvalid             = model.ErrNameInvalid
	ErrNotFound                = model.ErrNotFound
	ErrReferenceInvalid        = model.ErrReferenceInvalid
	ErrVersionConflict         = model.ErrVersionConflict
)

var (
	BuildReplacementPlan         = model.BuildReplacementPlan
	ValidateActiveReferenceFacts = model.ValidateActiveReferenceFacts
	ReferenceDiff                = model.ReferenceDiff
	ReferenceIDs                 = model.ReferenceIDs
	ReferencesEqual              = model.ReferencesEqual
	CommonTagNames               = model.CommonTagNames
)
