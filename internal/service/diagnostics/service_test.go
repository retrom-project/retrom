package diagnostics

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/diagnostics"
)

type memoryRepository struct {
	report model.Report
	err    error
}

func (repository memoryRepository) LoadReport(context.Context) (model.Report, error) {
	return repository.report, repository.err
}

func TestReportUsesSnapshotProjections(t *testing.T) {
	t.Parallel()
	service := New(memoryRepository{report: model.Report{
		DatabaseSchemaVersion: 14,
		Counts:                model.Counts{PublishedGames: 3, ReadyDATs: 2},
		RuntimeProviders:      []model.RuntimeProvider{{ProviderID: "runtime"}},
	}})
	report, err := service.Report(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.DatabaseSchemaVersion != 14 || report.Counts.PublishedGames != 3 ||
		len(report.RuntimeProviders) != 1 || report.RuntimeProviders[0].ProviderID != "runtime" {
		t.Fatalf("report = %#v", report)
	}
}

func TestReportPreservesRepositoryFailure(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("storage unavailable")
	_, err := New(memoryRepository{err: sentinel}).Report(t.Context())
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}
