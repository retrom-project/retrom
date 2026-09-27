package storageanalysis

import (
	"errors"
	"fmt"
)

var (
	errRetainedFileMissing  = errors.New("STORAGE_ANALYSIS_RETAINED_FILE_MISSING")
	errTotalInvariant       = errors.New("STORAGE_ANALYSIS_TOTAL_INVARIANT_FAILED")
	errCategoryInvariant    = errors.New("STORAGE_ANALYSIS_CATEGORY_INVARIANT_FAILED")
	errCandidateBlobMissing = errors.New("STORAGE_ANALYSIS_CANDIDATE_BLOB_MISSING")
	errSaveBlobMissing      = errors.New("STORAGE_ANALYSIS_SAVE_BLOB_MISSING")
)

func aggregate(
	blobs map[string]int64,
	retained map[string]struct{},
	usageByID map[string]Usage,
) (Snapshot, error) {
	index := make(map[CategoryCode]int, len(categoryOrder))
	categories := make([]Category, len(categoryOrder))
	for position, code := range categoryOrder {
		index[code] = position
		categories[position].Code = code
	}
	for id := range retained {
		if _, ok := blobs[id]; !ok {
			return Snapshot{}, fmt.Errorf("storageanalysis/service: %w", errRetainedFileMissing)
		}
	}
	var totals Totals
	for id, size := range blobs {
		if id == "" || size < 0 {
			return Snapshot{}, errTotalInvariant
		}
		var err error
		totals.RegisteredBytes, err = addChecked(totals.RegisteredBytes, size)
		if err != nil {
			return Snapshot{}, err
		}
		totals.FileCount, err = addChecked(totals.FileCount, 1)
		if err != nil {
			return Snapshot{}, err
		}
		_, isRetained := retained[id]
		code, err := classify(isRetained, usageByID[id])
		if err != nil {
			return Snapshot{}, err
		}
		category := &categories[index[code]]
		category.Bytes, err = addChecked(category.Bytes, size)
		if err != nil {
			return Snapshot{}, err
		}
		category.FileCount, err = addChecked(category.FileCount, 1)
		if err != nil {
			return Snapshot{}, err
		}
		if isRetained {
			totals.RetainedBytes, err = addChecked(totals.RetainedBytes, size)
		} else {
			totals.PendingDeleteBytes, err = addChecked(totals.PendingDeleteBytes, size)
		}
		if err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{Totals: totals, Categories: categories}, validateTotals(totals, categories)
}

func validateTotals(totals Totals, categories []Category) error {
	retainedAndPendingDelete, err := addChecked(totals.RetainedBytes, totals.PendingDeleteBytes)
	if err != nil || retainedAndPendingDelete != totals.RegisteredBytes {
		return fmt.Errorf("storageanalysis/service: %w", errTotalInvariant)
	}
	var categoryBytes int64
	var categoryCount int64
	for _, category := range categories {
		categoryBytes, err = addChecked(categoryBytes, category.Bytes)
		if err != nil {
			return err
		}
		categoryCount, err = addChecked(categoryCount, category.FileCount)
		if err != nil {
			return err
		}
	}
	if categoryBytes != totals.RegisteredBytes || categoryCount != totals.FileCount {
		return fmt.Errorf("storageanalysis/service: %w", errCategoryInvariant)
	}
	return nil
}
