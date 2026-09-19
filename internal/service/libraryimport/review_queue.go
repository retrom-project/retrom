package libraryimport

import (
	"context"
	"fmt"
	"strings"

	model "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
)

func NewReviewQueue(repository model.ReviewQueueRepository, tags model.ReviewQueueTags) *ReviewQueue {
	return &ReviewQueue{repository: repository, tags: tags}
}

func NormalizeReviewQueueFilter(filter model.ReviewQueueFilter) (model.ReviewQueueFilter, error) {
	filter.Query = strings.ToLower(strings.Join(strings.Fields(filter.Query), " "))
	if len([]rune(filter.Query)) > 200 || filter.TagID != "" && !taggingmodel.ValidID(filter.TagID) {
		return model.ReviewQueueFilter{}, model.ErrReviewQuery
	}
	sourceCount := 0
	for _, id := range []string{filter.ImportJobID, filter.PegasusImportID, filter.EmulationStationImportID} {
		if id != "" {
			sourceCount++
		}
	}
	if sourceCount > 1 {
		return model.ReviewQueueFilter{}, model.ErrReviewQuery
	}
	if filter.Sort == "" {
		filter.Sort = "UPDATED_ASC"
	}
	if filter.Sort != "UPDATED_ASC" && filter.Sort != "UPDATED_DESC" {
		return model.ReviewQueueFilter{}, model.ErrReviewQuery
	}
	if filter.Limit == 0 {
		filter.Limit = model.ReviewQueuePageLimit
	}
	if filter.Limit < 1 || filter.Limit > model.ReviewQueuePageLimit {
		return model.ReviewQueueFilter{}, model.ErrReviewQuery
	}
	return filter, nil
}

func (service *ReviewQueue) List(
	ctx context.Context, filter model.ReviewQueueFilter, after *model.ReviewQueuePosition,
) (model.ReviewQueuePage, error) {
	filter, err := NormalizeReviewQueueFilter(filter)
	if err != nil {
		return model.ReviewQueuePage{}, err
	}
	if after != nil && (after.ItemID == "" || after.UpdatedAtMS < 0) {
		return model.ReviewQueuePage{}, model.ErrReviewQuery
	}
	records, err := service.repository.List(ctx, model.ReviewQueueQuery{
		Filter: filter,
		After:  after,
		Limit:  filter.Limit + 1,
	})
	if err != nil {
		return model.ReviewQueuePage{}, fmt.Errorf("read review queue: %w", err)
	}
	page := model.ReviewQueuePage{Items: make([]model.ReviewQueueItem, 0, min(len(records), filter.Limit))}
	if len(records) > filter.Limit {
		records = records[:filter.Limit]
		last := records[len(records)-1]
		page.Next = &model.ReviewQueuePosition{UpdatedAtMS: last.UpdatedAtMS, ItemID: last.ItemID}
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
		return model.ReviewQueuePage{}, fmt.Errorf("read review queue tags: %w", err)
	}
	for index := range page.Items {
		page.Items[index].Tags = append([]taggingmodel.Reference{}, references[page.Items[index].ItemID]...)
	}
	return page, nil
}

func projectReviewQueueItem(record model.ReviewQueueRecord) model.ReviewQueueItem {
	item := model.ReviewQueueItem{
		ItemID: record.ItemID, ReviewVersion: record.Version, ImportJobID: record.ImportJobID,
		SourceDisplayName: record.SourceName, DraftTitle: record.DraftTitle, PlatformInstance: record.Platform,
		ValidationStatus: "NEEDS_VALIDATION", BlockerCodes: []string{}, CandidateCount: record.CandidateCount,
		SourceTotalSizeBytes: record.SourceTotalSizeBytes, SourceMD5: record.SourceMD5, UpdatedAtMS: record.UpdatedAtMS,
		SourceKind: "STANDARD", Tags: []taggingmodel.Reference{},
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
	if record.Pegasus != nil {
		item.SourceKind = "PEGASUS"
		item.PegasusImportID = &record.Pegasus.ImportID
		projectReviewQueueSource(&item, record.Pegasus)
	} else if record.EmulationStation != nil {
		item.SourceKind = "EMULATIONSTATION"
		item.EmulationStationImportID = &record.EmulationStation.ImportID
		projectReviewQueueSource(&item, record.EmulationStation)
	}
	return item
}

func projectReviewQueueSource(item *model.ReviewQueueItem, source *model.ReviewQueueSource) {
	item.SourceLabel = source.Label
	if item.CoverURL == nil && source.HasCover {
		url := "/api/v1/admin/review-assets/" + source.ItemID + "?kind=COVER"
		item.CoverURL = &url
	}
}
