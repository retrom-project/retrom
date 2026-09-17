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
	var report model.Report
	err := service.repository.WithRead(ctx, func(scope model.ReadScope) error {
		var err error
		report.DatabaseSchemaVersion, err = scope.SchemaVersion(ctx)
		if err != nil {
			return fmt.Errorf("read schema version: %w", err)
		}
		report.Counts, err = scope.Counts(ctx)
		if err != nil {
			return fmt.Errorf("read counts: %w", err)
		}
		report.RuntimeProviders, err = scope.RuntimeProviders(ctx)
		if err != nil {
			return fmt.Errorf("read runtime providers: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Report{}, fmt.Errorf("diagnostics report: %w", err)
	}
	return report, nil
}
