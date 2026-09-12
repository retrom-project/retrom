package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type (
	importGroupTargetGuard    = application.ImportTargetGuard
	importGroupTargetSnapshot = application.ImportTargetSnapshot
	queuedImportGroupRequest  = application.QueuedImportRequest
)

// QueueCreate is the compatibility entry to the import admission use case.
func (service *Service) QueueCreate(ctx context.Context, request CreateRequest) (Created, error) {
	admissions := application.NewImportAdmissions(repository.NewImportAdmissions(service.database), service, service.tags,
		application.ImportAdmissionOptions{
			Now: service.now, MultiDiscEnabled: service.multiDiscImportEnabled, MetadataScraperAvailable: service.scraper != nil,
		})
	result, err := admissions.Queue(ctx, request)
	if err != nil {
		return Created{}, fmt.Errorf("queue import: %w", err)
	}
	return result, nil
}

// NotifyImportGroup wakes the existing worker only after admission commits.
func (service *Service) NotifyImportGroup(ctx context.Context, jobID string) {
	service.scheduleImportGroup(ctx, jobID, 0)
}

func targetGuard(target creationTarget) importGroupTargetGuard {
	return application.TargetImportGuard(importTargetFacts(target))
}

func queuedPrincipalContext(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}
	return authn.WithPrincipal(ctx, authn.Principal{UserID: userID})
}
