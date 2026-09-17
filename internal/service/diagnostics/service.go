package diagnostics

import (
	"context"
	"errors"
	"fmt"

	model "retrom/internal/model/diagnostics"
)

var ErrUnavailable = errors.New("diagnostics repository unavailable")

// Service assembles a diagnostics report from one repository snapshot.
type Service struct {
	repository model.Repository
}

func New(repository model.Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) Report(ctx context.Context) (model.Report, error) {
	if service.repository == nil {
		return model.Report{}, ErrUnavailable
	}
	report, err := service.repository.LoadReport(ctx)
	if err != nil {
		return model.Report{}, fmt.Errorf("diagnostics report: %w", err)
	}
	return report, nil
}
