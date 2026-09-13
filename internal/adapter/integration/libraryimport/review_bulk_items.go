package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	librarypersistence "retrom/internal/repo/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

// ListReviewBulkItems is retained as a compatibility facade while the typed
// query and row mapping live in the service/persistence layers.
func (service *Service) ListReviewBulkItems(
	ctx context.Context,
	bulkID, outcome, cursor string,
	limit int,
) (ReviewBulkItemPage, error) {
	page, err := libraryservice.NewReviewBulkQueries(
		librarypersistence.BindReviewBulkQueries(service.database),
	).Items(ctx, bulkID, outcome, cursor, limit)
	if err != nil {
		if errors.Is(err, libraryservice.ErrReviewBulkQuery) {
			return ReviewBulkItemPage{}, ErrReviewBulkConflict
		}
		return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk items: %w", err)
	}
	items := make([]ReviewBulkItemResult, 0, len(page.Items))
	for _, item := range page.Items {
		var details any
		if len(item.OutcomeDetails) > 0 {
			if err := json.Unmarshal(item.OutcomeDetails, &details); err != nil {
				return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk item details: %w", err)
			}
		}
		items = append(items, ReviewBulkItemResult{
			ImportItemID: item.ImportItemID, Title: item.Title, PlatformName: item.PlatformName,
			State: item.State, GameID: item.GameID, ReviewEventID: item.ReviewEventID,
			OutcomeCode: item.OutcomeCode, OutcomeDetails: details, CompletedAtMS: item.CompletedAtMS,
		})
	}
	return ReviewBulkItemPage{Items: items, NextCursor: page.NextCursor}, nil
}
