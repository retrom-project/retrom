package maintenance

import (
	"context"

	"retrom/internal/model/payloadrelease"
)

type RestoredImportScope struct {
	Reviews  RestoredReviewScope
	Payloads RestoredPayloadScope
}

type RestoredPayloadQuery struct {
	Kind    payloadrelease.ScopeType
	AfterID string
	Limit   int
}

type RestoredPayloadRecords interface {
	RetainedSources(context.Context, RestoredPayloadQuery) ([]string, error)
}

type RestoredPayloadScope struct {
	Records    RestoredPayloadRecords
	Scheduling payloadrelease.SchedulingScope
}
