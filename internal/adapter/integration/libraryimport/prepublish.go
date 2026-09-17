package libraryimport

import (
	"context"
	"fmt"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

type prepublishDigestInput = libraryimportmodel.PrepublishDigestInput

func prepublishDigest(input prepublishDigestInput) string {
	return libraryimportmodel.PrepublishDigest(input)
}

func prepublishDigestMatches(digest string, input prepublishDigestInput) bool {
	return libraryimportmodel.PrepublishDigestMatches(digest, input)
}

func preparedGroupContentKind(group preparedGroup) string {
	return libraryimportservice.PreparedGroupContentKind(group)
}

type reviewValidationEvidence = libraryimportmodel.ReviewValidationEvidence

func (service *Service) ReviewValidationCurrent(ctx context.Context, validationID string) (bool, error) {
	validation := libraryimportservice.NewReviewValidation(repository.BindReviewValidation(service.database))
	current, err := validation.Current(ctx, validationID)
	if err != nil {
		return false, fmt.Errorf("read current review validation: %w", err)
	}
	return current, nil
}
