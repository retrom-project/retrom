package libraryimport

import (
	"context"
	"errors"

	"retrom/internal/service/tagging"
)

var ErrReviewQuery = errors.New("INVALID_REVIEW_QUERY")

const ReviewQueuePageLimit = 20

type (
	ReviewQueueFilter struct {
		Query, TagID, ImportJobID, PegasusImportID, EmulationStationImportID string
		PlatformInstanceID, BlockerCode, Sort                                string
		Limit                                                                int
	}
	ReviewQueuePosition struct {
		UpdatedAtMS int64
		ItemID      string
	}
	ReviewQueueQuery struct {
		Filter ReviewQueueFilter
		After  *ReviewQueuePosition
		Limit  int
	}
	ReviewQueuePlatform struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ReviewQueueSource struct {
		ItemID, ImportID string
		Label            *string
		HasCover         bool
	}
	ReviewQueueRecord struct {
		ItemID, ImportJobID, DraftTitle, SourceName                  string
		Version, UpdatedAtMS, CandidateCount, SourceTotalSizeBytes   int64
		Platform                                                     ReviewQueuePlatform
		ValidationStatus, CompatibilityCode, SourceMD5, CoverAssetID *string
		Pegasus, EmulationStation                                    *ReviewQueueSource
	}
	ReviewQueueItem struct {
		ItemID                   string              `json:"itemId"`
		ReviewVersion            int64               `json:"reviewVersion"`
		ImportJobID              string              `json:"importJobId"`
		SourceDisplayName        string              `json:"sourceDisplayName"`
		DraftTitle               string              `json:"draftTitle"`
		PlatformInstance         ReviewQueuePlatform `json:"platformInstance"`
		ValidationStatus         string              `json:"validationStatus"`
		ValidationJobID          *string             `json:"validationJobId"`
		BlockerCodes             []string            `json:"blockerCodes"`
		CandidateCount           int64               `json:"candidateCount"`
		SourceTotalSizeBytes     int64               `json:"sourceTotalSizeBytes"`
		SourceMD5                *string             `json:"sourceMd5"`
		CoverURL                 *string             `json:"coverUrl"`
		SourceKind               string              `json:"sourceKind"`
		SourceLabel              *string             `json:"sourceLabel"`
		PegasusImportID          *string             `json:"pegasusImportId"`
		EmulationStationImportID *string             `json:"emulationStationImportId"`
		UpdatedAtMS              int64               `json:"updatedAtMs"`
		Tags                     []tagging.Reference `json:"tags"`
	}
	ReviewQueuePage struct {
		Items []ReviewQueueItem
		Next  *ReviewQueuePosition
	}
	ReviewQueueRepository interface {
		List(context.Context, ReviewQueueQuery) ([]ReviewQueueRecord, error)
	}
	ReviewQueueTags interface {
		ReviewReferences(context.Context, []string) (map[string][]tagging.Reference, error)
	}
	ReviewQueue struct {
		repository ReviewQueueRepository
		tags       ReviewQueueTags
	}
)
