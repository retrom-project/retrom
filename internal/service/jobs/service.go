package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"
)


type Service struct {
	repository    Repository
	cancellations map[string]DomainCanceller
	now           func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func validReason(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len([]rune(value)) <= 500
}

func (service *Service) Cancel(
	ctx context.Context, jobID string, expectedVersion int64, reason string,
) (Result, bool, error) {
	if !validReason(reason) {
		return Result{}, false, ErrConflict
	}
	trimmed := strings.TrimSpace(reason)
	domainKinds := make([]string, 0, len(service.cancellations))
	for k := range service.cancellations {
		domainKinds = append(domainKinds, k)
	}
	cr, err := service.repository.CommitCancel(ctx, CancelCommand{
		JobID: jobID, ExpectedVersion: expectedVersion,
		Reason: trimmed, NowMS: service.now().UnixMilli(),
		DomainHandlerFor: domainKinds,
	})
	if err != nil {
		return Result{}, false, fmt.Errorf("jobs/cancel: %w", err)
	}
	if cr.NeedsDomain {
		handler := service.cancellations[cr.DomainJob.Kind]
		if handler != nil {
			return domainDispatch{
				handler: handler,
				command: DomainCancellation{
					JobID: jobID, Kind: cr.DomainJob.Kind,
					ScopeID: cr.DomainJob.ScopeID,
					Reason: trimmed, ExpectedVersion: expectedVersion,
				},
			}.cancel(ctx)
		}
	}
	return cr.Result, cr.Pending, nil
}
