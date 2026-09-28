package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

type prepublishDigestInput = libraryservice.PrepublishDigestInput

func prepublishDigest(input prepublishDigestInput) string {
	return libraryservice.PrepublishDigest(input)
}

func prepublishDigestMatches(digest string, input prepublishDigestInput) bool {
	return libraryservice.PrepublishDigestMatches(digest, input)
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	valueCopy := value.String
	return &valueCopy
}

func preparedGroupContentKind(group preparedGroup) string {
	return libraryservice.PreparedGroupContentKind(group)
}

type reviewValidationEvidence = libraryservice.ReviewValidationEvidence

func (service *Service) ReviewValidationCurrent(ctx context.Context, validationID string) (bool, error) {
	validation := libraryservice.NewReviewValidation(repository.BindReviewValidation(service.database))
	current, err := validation.Current(ctx, validationID)
	if err != nil {
		return false, fmt.Errorf("read current review validation: %w", err)
	}
	return current, nil
}
