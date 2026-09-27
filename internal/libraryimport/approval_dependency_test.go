package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"

	application "retrom/internal/service/libraryimport"
)

var errInjectedApproval = errors.New("injected approval repository")

type injectedApprovalRepository struct{ calls int }

func (*injectedApprovalRepository) PendingPublications(context.Context) ([]application.ReviewApprovalRequest, error) {
	return nil, errInjectedApproval
}

func (repository *injectedApprovalRepository) WithApproval(context.Context, func(application.ReviewApprovalScope) error) error {
	repository.calls++
	return errInjectedApproval
}

func TestImporterUsesSuppliedApprovalAcrossRequests(t *testing.T) {
	fixture := newTestImporter(t, nil, nil, testImportOptions{})
	deps := assembleTestDependencies(testImportOptions{
		Database: fixture.database, Files: fixture.blobs, Tags: fixture.tags, Now: time.Now,
	})
	repository := &injectedApprovalRepository{}
	deps.Approvals = application.NewReviewApprovals(repository, fixture.tags, time.Now, fixture.blobs)
	importer := New(deps, Options{})
	t.Cleanup(importer.Close)
	for range 2 {
		_, err := importer.Approve(t.Context(), "01980000-0000-7000-8000-000000000001", 1)
		if !errors.Is(err, errInjectedApproval) {
			t.Fatalf("approval bypassed injected collaborator: %v", err)
		}
	}
	if repository.calls != 2 {
		t.Fatalf("approval calls=%d", repository.calls)
	}
}
