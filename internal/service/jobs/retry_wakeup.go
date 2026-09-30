package jobs

import "context"

type RetryWakeup func(context.Context, string)

// WithRetryWakeups binds domain workers explicitly before publishing the service.
func (service *Service) WithRetryWakeups(handlers map[string]RetryWakeup) *Service {
	configured := make(map[string]RetryWakeup, len(service.retryWakeups)+len(handlers))
	for kind, handler := range service.retryWakeups {
		configured[kind] = handler
	}
	for kind, handler := range handlers {
		configured[kind] = handler
	}
	result := *service
	result.retryWakeups = configured
	return &result
}

// WakeRetry runs only after the request's idempotency receipt has committed.
// Workers recover from their durable queues if this notification is lost.
func (service *Service) WakeRetry(ctx context.Context, result Result) {
	if handler := service.retryWakeups[result.Kind]; handler != nil {
		handler(ctx, result.JobID)
	}
}
