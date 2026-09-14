package maintenance

import model "retrom/internal/model/maintenance"

type (
	AccessCounts           = model.AccessCounts
	Blob                   = model.Blob
	FenceAudit             = model.FenceAudit
	FenceCounts            = model.FenceCounts
	ImportCounts           = model.ImportCounts
	Lineage                = model.Lineage
	Repository             = model.Repository
	RestoreRecords         = model.RestoreRecords
	RestoredImportScope    = model.RestoredImportScope
	RestoredPayloadQuery   = model.RestoredPayloadQuery
	RestoredPayloadRecords = model.RestoredPayloadRecords
	RestoredPayloadScope   = model.RestoredPayloadScope
	RestoredReview         = model.RestoredReview
	RestoredReviewChange   = model.RestoredReviewChange
	RestoredReviewQuery    = model.RestoredReviewQuery
	RestoredReviewRecords  = model.RestoredReviewRecords
	RestoredReviewScope    = model.RestoredReviewScope
	Snapshot               = model.Snapshot
	UploadPart             = model.UploadPart
)

var (
	ErrInvalidBundle    = model.ErrInvalidBundle
	ErrCheckpointFailed = model.ErrCheckpointFailed
)
