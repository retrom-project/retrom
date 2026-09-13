package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type prepublishDigestInput = application.PrepublishDigestInput

func prepublishDigest(input prepublishDigestInput) string { return application.PrepublishDigest(input) }

func prepublishDigestMatches(digest string, input prepublishDigestInput) bool {
	return application.PrepublishDigestMatches(digest, input)
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	valueCopy := value.String
	return &valueCopy
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	valueCopy := value
	return &valueCopy
}

func preparedGroupContentKind(group preparedGroup) string {
	return application.PreparedGroupContentKind(group)
}

type reviewValidationEvidence = application.ReviewValidationEvidence

func (service *Service) ReviewValidationCurrent(ctx context.Context, validationID string) (bool, error) {
	validation := application.NewReviewValidation(repository.BindReviewValidation(service.database))
	current, err := validation.Current(ctx, validationID)
	if err != nil {
		return false, fmt.Errorf("read current review validation: %w", err)
	}
	return current, nil
}
