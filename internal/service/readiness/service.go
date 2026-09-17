package readiness

import (
	"context"
	model "retrom/internal/model/readiness"
	"time"
)

const probeTimeout = 2 * time.Second

type Service struct{ repository model.Repository }

func New(repository model.Repository) *Service { return &Service{repository: repository} }

func (service *Service) Reason(ctx context.Context) string {
	if service == nil || service.repository == nil {
		return "DATABASE_UNAVAILABLE"
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	status, err := service.repository.Check(ctx)
	if err != nil {
		return "DATABASE_UNAVAILABLE"
	}
	if status.Missing == 0 {
		return ""
	}
	if status.Failed > 0 {
		return "DEPENDENCY_DAT_PARSE_FAILED"
	}
	return "DEPENDENCY_INDEXING"
}
