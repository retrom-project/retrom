package metadatascrape

import "context"

type MediaQueueWriter interface {
	Enqueue(context.Context, MediaJobPlan) error
}
