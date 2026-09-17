package jobs

import (
	"context"
	"fmt"

	model "retrom/internal/model/jobs"
)

type DomainCancellation struct {
	JobID, Kind, ScopeID, Reason string
	ExpectedVersion              int64
}

type DomainCanceller interface {
	CancelJob(context.Context, DomainCancellation) (model.Result, bool, error)
}

// WithDomainCancellation returns a service with a copied registry, preserving
// existing registrations. Configure handlers before publishing the service.
func (service *Service) WithDomainCancellation(handlers map[string]DomainCanceller) *Service {
	configured := make(map[string]DomainCanceller, len(service.cancellations)+len(handlers))
	for kind, handler := range service.cancellations {
		configured[kind] = handler
	}
	for kind, handler := range handlers {
		if handler != nil {
			configured[kind] = handler
		}
	}
	result := *service
	result.cancellations = configured
	return &result
}

type domainDispatch struct {
	handler DomainCanceller
	command DomainCancellation
}

func (dispatch domainDispatch) cancel(ctx context.Context) (model.Result, bool, error) {
	result, pending, err := dispatch.handler.CancelJob(ctx, dispatch.command)
	if err != nil {
		return model.Result{}, false, fmt.Errorf("cancel domain job: %w", err)
	}
	return result, pending, nil
}
