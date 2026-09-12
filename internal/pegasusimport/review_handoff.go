package pegasusimport

import (
	"errors"
	"os"
	"strings"

	repository "retrom/internal/persistence/pegasusimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/libraryimport"
)

func (service *Service) reviewPreparation() *application.ReviewPreparation {
	return application.NewReviewPreparation(service.importer,
		application.NewItemWork(repository.NewItemWork(service.database), service.now),
		application.NewReviewHandoff(repository.NewReviewHandoff(service.database),
			libraryservice.NewMetadataSeeder(nil, service.now), service.now))
}

func (service *Service) itemFailure(stage, operation string, err error, relativePath string) *FailureDetails {
	return application.DescribeFailure(importDiagnostics{service}, stage, operation, err, relativePath)
}

func (service *Service) libraryImportFailure(err error, files []libraryimport.ServerSourceFile) *FailureDetails {
	return application.LibraryFailureDetails(importDiagnostics{service}, err, files)
}

func (service *Service) sanitizeTechnicalDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := err.Error()
	var pathError *os.PathError
	if errors.As(err, &pathError) && pathError.Path != "" {
		detail = strings.ReplaceAll(detail, pathError.Path, "[path]")
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		if linkError.Old != "" {
			detail = strings.ReplaceAll(detail, linkError.Old, "[path]")
		}
		if linkError.New != "" {
			detail = strings.ReplaceAll(detail, linkError.New, "[path]")
		}
	}
	for _, root := range service.roots {
		if root.path != "" {
			detail = strings.ReplaceAll(detail, root.path, "[server-root]")
		}
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	return detail
}
