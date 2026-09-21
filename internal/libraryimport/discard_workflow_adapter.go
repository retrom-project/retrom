package libraryimport

import "context"

// DiscardWorkflow adapts the legacy import facade to the application-level
// workflow used by the import-discard coordinator. Keeping this adapter at the
// legacy boundary lets composition depend on service ports without exposing
// legacy result types there.
type DiscardWorkflow struct{ service *Service }

func NewDiscardWorkflow(service *Service) *DiscardWorkflow {
	return &DiscardWorkflow{service: service}
}

func (workflow *DiscardWorkflow) CancelForDiscard(ctx context.Context, id string, version int64) error {
	if workflow == nil || workflow.service == nil {
		return ErrInvalid
	}
	_, _, err := workflow.service.CancelForDiscard(ctx, id, version)
	return err
}

func (workflow *DiscardWorkflow) DiscardBatchReviews(ctx context.Context, id string) (bool, error) {
	if workflow == nil || workflow.service == nil {
		return false, ErrInvalid
	}
	return workflow.service.DiscardBatchReviews(ctx, id)
}

func (workflow *DiscardWorkflow) ReleaseDiscardedBatch(ctx context.Context, id string) error {
	if workflow == nil || workflow.service == nil {
		return ErrInvalid
	}
	return workflow.service.ReleaseDiscardedBatch(ctx, id)
}
