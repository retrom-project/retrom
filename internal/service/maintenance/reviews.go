package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	model "retrom/internal/model/maintenance"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	libraryimportservice "retrom/internal/service/libraryimport"
	pegasus "retrom/internal/service/pegasusimport"

	// CompleteRestoredReviews preserves already materialized ordinary reviews without
	// reading the former host's sources. The caller owns the entire restore transaction,
	// including access revocation, external job termination and the security audit.
	libraryimportmodel "retrom/internal/model/libraryimport"
)

func CompleteRestoredReviews(ctx context.Context, scope model.RestoredReviewScope, now time.Time) error {
	for _, kind := range []string{"PEGASUS", "EMULATIONSTATION"} {
		query := model.RestoredReviewQuery{Kind: kind, Limit: 100}
		for {
			reviews, err := scope.Records.Pending(ctx, query)
			if err != nil {
				return fmt.Errorf("read restored reviews: %w", err)
			}
			if len(reviews) == 0 {
				break
			}
			for _, review := range reviews {
				if review.Kind != kind || review.ItemID <= query.AfterID {
					return model.ErrInvalidBundle
				}
				if err := completeRestoredReview(ctx, scope, review, now); err != nil {
					return fmt.Errorf("complete restored review: %w", err)
				}
				query.AfterID = review.ItemID
			}
		}
	}
	return nil
}

func completeRestoredReview(
	ctx context.Context, scope model.RestoredReviewScope, review model.RestoredReview, now time.Time,
) error {
	if !validRestoredReview(review) {
		return model.ErrInvalidBundle
	}
	preparation, err := restoredReviewPreparation(review)
	if err != nil {
		return err
	}
	if review.Version > math.MaxInt64-int64(len(preparation))-1 {
		return model.ErrInvalidBundle
	}
	var metadata libraryimportmodel.ServerMetadata
	if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
		return fmt.Errorf("decode restored source metadata: %w", err)
	}
	maximumYear := review.ReleaseYearMax
	if review.Kind == "PEGASUS" {
		maximumYear = now.UTC().Year() + 1
	}
	_, additions, err := libraryimportservice.NewMetadataSeeder(nil, func() time.Time { return now }).SeedInScope(
		ctx, scope.Metadata, review.ReservedItemID, metadata, maximumYear)
	if err != nil {
		return fmt.Errorf("seed restored review metadata: %w", err)
	}
	warnings, err := restoredReviewWarnings(review, additions)
	if err != nil {
		return err
	}
	err = scope.Records.Complete(ctx, model.RestoredReviewChange{
		Before: review, Preparation: preparation, WarningsJSON: warnings, NowMS: now.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("persist restored review handoff: %w", err)
	}
	return nil
}

func validRestoredReview(review model.RestoredReview) bool {
	if !validRestoredReviewIdentity(review) || !validRestoredReviewBinding(review) {
		return false
	}
	if !activeRestoredImport(review.ImportState) || !activeRestoredImport(review.JobState) {
		return false
	}
	if review.Kind == "PEGASUS" {
		return review.LibraryItemID != ""
	}
	return review.Kind == "EMULATIONSTATION" && review.UploadID != "" && review.OwnerUpload == review.UploadID &&
		review.ReleaseYearMax > 0
}

func validRestoredReviewIdentity(review model.RestoredReview) bool {
	for _, id := range []string{
		review.ItemID, review.ImportID, review.JobID, review.ReservedJobID, review.ReservedItemID,
	} {
		if id == "" {
			return false
		}
	}
	for _, version := range []int64{
		review.Version, review.ImportVersion, review.JobVersion,
		review.ExecutionNo, review.OrdinaryVersion,
	} {
		if version < 1 || version == math.MaxInt64 {
			return false
		}
	}
	return review.OrdinaryItemCount == 1
}

func validRestoredReviewBinding(review model.RestoredReview) bool {
	if review.OwnerUpload != "" && (review.OwnerUpload != review.UploadID ||
		review.OwnerKind != review.Kind || review.OwnerItemID != review.ItemID) {
		return false
	}
	if review.LibraryJobID != "" || review.LibraryItemID != "" {
		if review.LibraryJobID != review.ReservedJobID || review.LibraryItemID != review.ReservedItemID {
			return false
		}
	}
	return true
}

func activeRestoredImport(state string) bool {
	return state == "QUEUED" || state == "RUNNING" || state == "CANCEL_REQUESTED"
}

func restoredReviewPreparation(review model.RestoredReview) ([]string, error) {
	if review.Kind == "EMULATIONSTATION" {
		states, err := emulationstationimportservice.ReviewPreparation(
			emulationstationimportmodel.ExecutionReview{State: review.State, Retryable: review.Retryable},
		)
		if err != nil {
			return nil, fmt.Errorf("prepare restored EmulationStation review: %w", err)
		}
		return states, nil
	}
	switch review.State {
	case "PENDING":
		return []string{"COPYING", "VALIDATING"}, nil
	case "COPYING":
		return []string{"VALIDATING"}, nil
	case "VALIDATING":
		return nil, nil
	default:
		return nil, model.ErrInvalidBundle
	}
}

func restoredReviewWarnings(
	review model.RestoredReview,
	additions []libraryimportmodel.ServerMetadataWarning,
) (string, error) {
	if review.Kind == "EMULATIONSTATION" {
		warnings, err := emulationstationimportservice.AppendReviewMetadataWarnings(review.WarningsJSON, additions)
		if err != nil {
			return "", fmt.Errorf("merge restored EmulationStation warnings: %w", err)
		}
		return warnings, nil
	}
	var warnings []map[string]any
	if err := json.Unmarshal([]byte(review.WarningsJSON), &warnings); err != nil {
		return "", fmt.Errorf("decode restored Pegasus warnings: %w", err)
	}
	if warnings == nil {
		return "", model.ErrInvalidBundle
	}
	encoded, err := json.Marshal(pegasus.MergeReviewMetadataWarnings(warnings, additions))
	if err != nil {
		return "", fmt.Errorf("encode restored Pegasus warnings: %w", err)
	}
	return string(encoded), nil
}
