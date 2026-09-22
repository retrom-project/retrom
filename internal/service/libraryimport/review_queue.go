package libraryimport

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/service/tagging"
)

func NewReviewQueue(repository ReviewQueueRepository, tags ReviewQueueTags) *ReviewQueue {
	return &ReviewQueue{repository: repository, tags: tags}
}

func NormalizeReviewQueueFilter(filter ReviewQueueFilter) (ReviewQueueFilter, error) {
	filter.Query = strings.ToLower(strings.Join(strings.Fields(filter.Query), " "))
	if len([]rune(filter.Query)) > 200 || filter.TagID != "" && !tagging.ValidID(filter.TagID) {
		return ReviewQueueFilter{}, ErrReviewQuery
	}
	sourceCount := 0
	for _, id := range []string{filter.ImportJobID, filter.SourceImportID} {
		if id != "" {
			sourceCount++
		}
	}
	if sourceCount > 1 {
		return ReviewQueueFilter{}, ErrReviewQuery
	}
	if filter.Sort == "" {
		filter.Sort = "UPDATED_ASC"
	}
	if filter.Sort != "UPDATED_ASC" && filter.Sort != "UPDATED_DESC" {
		return ReviewQueueFilter{}, ErrReviewQuery
	}
	if filter.Limit == 0 {
		filter.Limit = ReviewQueuePageLimit
	}
	if filter.Limit < 1 || filter.Limit > ReviewQueuePageLimit {
		return ReviewQueueFilter{}, ErrReviewQuery
	}
	return filter, nil
}

func (service *ReviewQueue) List(
	ctx context.Context, filter ReviewQueueFilter, after *ReviewQueuePosition,
) (ReviewQueuePage, error) {
	filter, err := NormalizeReviewQueueFilter(filter)
	if err != nil {
		return ReviewQueuePage{}, err
	}
	if after != nil && (after.ItemID == "" || after.UpdatedAtMS < 0) {
		return ReviewQueuePage{}, ErrReviewQuery
	}
	records, err := service.repository.List(ctx, ReviewQueueQuery{Filter: filter, After: after, Limit: filter.Limit + 1})
	if err != nil {
		return ReviewQueuePage{}, fmt.Errorf("read review queue: %w", err)
	}
	page := ReviewQueuePage{Items: make([]ReviewQueueItem, 0, min(len(records), filter.Limit))}
	if len(records) > filter.Limit {
		records = records[:filter.Limit]
		last := records[len(records)-1]
		page.Next = &ReviewQueuePosition{UpdatedAtMS: last.UpdatedAtMS, ItemID: last.ItemID}
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		page.Items = append(page.Items, projectReviewQueueItem(record))
		ids = append(ids, record.ItemID)
	}
	if len(ids) == 0 {
		return page, nil
	}
	references, err := service.tags.ReviewReferences(ctx, ids)
	if err != nil {
		return ReviewQueuePage{}, fmt.Errorf("read review queue tags: %w", err)
	}
	for index := range page.Items {
		page.Items[index].Tags = append([]tagging.Reference{}, references[page.Items[index].ItemID]...)
	}
	return page, nil
}

func projectReviewQueueItem(record ReviewQueueRecord) ReviewQueueItem {
	item := ReviewQueueItem{
		ItemID: record.ItemID, ReviewVersion: record.Version, ImportJobID: record.ImportJobID,
		SourceDisplayName: record.SourceName, DraftTitle: record.DraftTitle, PlatformInstance: record.Platform,
		ValidationStatus: "NEEDS_VALIDATION", BlockerCodes: []string{}, CandidateCount: record.CandidateCount,
		SourceTotalSizeBytes: record.SourceTotalSizeBytes, SourceMD5: record.SourceMD5, UpdatedAtMS: record.UpdatedAtMS,
		SourceKind: "STANDARD", Tags: []tagging.Reference{},
	}
	if record.ValidationStatus != nil && *record.ValidationStatus != "" {
		item.ValidationStatus = *record.ValidationStatus
	}
	if item.ValidationStatus != "READY" && record.CompatibilityCode != nil {
		item.BlockerCodes = append(item.BlockerCodes, *record.CompatibilityCode)
	}
	if record.CoverAssetID != nil {
		url := "/api/v1/admin/review-assets/" + *record.CoverAssetID
		item.CoverURL = &url
	}
	if record.Source != nil {
		item.SourceKind = "SOURCE"
		item.SourceImportID = &record.Source.ImportID
		projectReviewQueueSource(&item, record.Source)
	}
	return item
}

func projectReviewQueueSource(item *ReviewQueueItem, source *ReviewQueueSource) {
	item.SourceLabel = source.Label
	if item.CoverURL == nil && source.HasCover {
		url := "/api/v1/admin/review-assets/" + source.ItemID + "?kind=COVER"
		item.CoverURL = &url
	}
}
