package diagnostics

import (
	"context"
	"errors"
	"testing"
)

type memoryRepository struct {
	scope ReadScope
	err   error
}

func (repository memoryRepository) WithRead(_ context.Context, work func(ReadScope) error) error {
	if repository.err != nil {
		return repository.err
	}
	return work(repository.scope)
}

type memoryScope struct {
	schema int64
	counts Counts
	items  []RuntimeProvider
}

func (scope memoryScope) SchemaVersion(context.Context) (int64, error) { return scope.schema, nil }
func (scope memoryScope) Counts(context.Context) (Counts, error)       { return scope.counts, nil }
func (scope memoryScope) RuntimeProviders(context.Context) ([]RuntimeProvider, error) {
	return scope.items, nil
}

func TestReportUsesSnapshotProjections(t *testing.T) {
	t.Parallel()
	service := New(memoryRepository{scope: memoryScope{
		schema: 14,
		counts: Counts{PublishedGames: 3, ReadyDATs: 2},
		items:  []RuntimeProvider{{ProviderID: "runtime"}},
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
