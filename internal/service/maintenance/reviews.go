package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	library "retrom/internal/service/libraryimport"
	source "retrom/internal/service/sourceimport"
)

type RestoredReview struct {
	Kind, ItemID, ImportID, JobID                        string
	State, ImportState, JobState                         string
	Version, ImportVersion, JobVersion, ExecutionNo      int64
	LibraryJobID, LibraryItemID                          string
	ReservedJobID, ReservedItemID, UploadID, OwnerUpload string
	OwnerKind, OwnerItemID                               string
	OrdinaryItemCount, OrdinaryVersion                   int64
	MetadataJSON, WarningsJSON                           string
	RootID, RootDigest, RelativePath, CreatorID          string
	ReleaseYearMax                                       int
	Retryable                                            bool
}

type RestoredReviewQuery struct {
	Kind, AfterID string
	Limit         int
}

type RestoredReviewChange struct {
	Before       RestoredReview
	Preparation  []string
	WarningsJSON string
	NowMS        int64
}

type RestoredReviewRecords interface {
	Pending(context.Context, RestoredReviewQuery) ([]RestoredReview, error)
	Complete(context.Context, RestoredReviewChange) error
}

type RestoredReviewScope struct {
	Records  RestoredReviewRecords
	Metadata library.MetadataScope
}

// CompleteRestoredReviews preserves already materialized ordinary reviews without
// reading the former host's sources. The caller owns the entire restore transaction,
// including access revocation, external job termination and the security audit.
func CompleteRestoredReviews(ctx context.Context, scope RestoredReviewScope, now time.Time) error {
	for _, kind := range []string{"SOURCE"} {
		query := RestoredReviewQuery{Kind: kind, Limit: 100}
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
					return ErrInvalidBundle
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
	ctx context.Context, scope RestoredReviewScope, review RestoredReview, now time.Time,
) error {
	if !validRestoredReview(review) {
		return ErrInvalidBundle
	}
	preparation, err := restoredReviewPreparation(review)
	if err != nil {
		return err
	}
	if review.Version > math.MaxInt64-int64(len(preparation))-1 {
		return ErrInvalidBundle
	}
	var metadata library.ServerMetadata
	if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
		return fmt.Errorf("decode restored source metadata: %w", err)
	}
	maximumYear := review.ReleaseYearMax
	if review.Kind == "SOURCE" {
		maximumYear = now.UTC().Year() + 1
	}
	_, additions, err := library.NewMetadataSeeder(nil, func() time.Time { return now }).SeedInScope(
		ctx, scope.Metadata, review.ReservedItemID, metadata, maximumYear)
	if err != nil {
		return fmt.Errorf("seed restored review metadata: %w", err)
	}
	warnings, err := restoredReviewWarnings(review, additions)
	if err != nil {
		return err
	}
	err = scope.Records.Complete(ctx, RestoredReviewChange{
		Before: review, Preparation: preparation, WarningsJSON: warnings, NowMS: now.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("persist restored review handoff: %w", err)
	}
	return nil
}

func validRestoredReview(review RestoredReview) bool {
	if !validRestoredReviewIdentity(review) || !validRestoredReviewBinding(review) {
		return false
	}
	if !activeRestoredImport(review.ImportState) || !activeRestoredImport(review.JobState) {
		return false
	}
	if review.Kind == "SOURCE" {
		return review.LibraryItemID != ""
	}
	return false
}

func validRestoredReviewIdentity(review RestoredReview) bool {
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

func validRestoredReviewBinding(review RestoredReview) bool {
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

func restoredReviewPreparation(review RestoredReview) ([]string, error) {
	switch review.State {
	case "PENDING":
		return []string{"COPYING", "VALIDATING"}, nil
	case "COPYING":
		return []string{"VALIDATING"}, nil
	case "VALIDATING":
		return nil, nil
	default:
		return nil, ErrInvalidBundle
	}
}

func restoredReviewWarnings(review RestoredReview, additions []library.ServerMetadataWarning) (string, error) {
	var warnings []map[string]any
	if err := json.Unmarshal([]byte(review.WarningsJSON), &warnings); err != nil {
		return "", fmt.Errorf("decode restored Source warnings: %w", err)
	}
	if warnings == nil {
		return "", ErrInvalidBundle
	}
	encoded, err := json.Marshal(source.MergeReviewMetadataWarnings(warnings, additions))
	if err != nil {
		return "", fmt.Errorf("encode restored Source warnings: %w", err)
	}
	return string(encoded), nil
}
