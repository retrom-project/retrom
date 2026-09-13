package storageanalysis

import (
	"errors"
	"fmt"
)

var (
	errProtectedBlobMissing = errors.New("STORAGE_ANALYSIS_PROTECTED_BLOB_MISSING")
	errTotalInvariant       = errors.New("STORAGE_ANALYSIS_TOTAL_INVARIANT_FAILED")
	errCategoryInvariant    = errors.New("STORAGE_ANALYSIS_CATEGORY_INVARIANT_FAILED")
	errCandidateBlobMissing = errors.New("STORAGE_ANALYSIS_CANDIDATE_BLOB_MISSING")
	errSaveBlobMissing      = errors.New("STORAGE_ANALYSIS_SAVE_BLOB_MISSING")
)

func aggregate(
	blobs map[string]int64,
	protected map[string]struct{},
	usageByID map[string]Usage,
) (Snapshot, error) {
	index := make(map[CategoryCode]int, len(categoryOrder))
	categories := make([]Category, len(categoryOrder))
	for position, code := range categoryOrder {
		index[code] = position
		categories[position].Code = code
	}
	for id := range protected {
		if _, ok := blobs[id]; !ok {
			return Snapshot{}, fmt.Errorf("storageanalysis/service: %w", errProtectedBlobMissing)
		}
	}
	var totals Totals
	for id, size := range blobs {
		var err error
		totals.RegisteredBytes, err = addChecked(totals.RegisteredBytes, size)
		if err != nil {
			return Snapshot{}, err
		}
		totals.BlobCount, err = addChecked(totals.BlobCount, 1)
		if err != nil {
			return Snapshot{}, err
		}
		_, isProtected := protected[id]
		code := classify(isProtected, usageByID[id])
		category := &categories[index[code]]
		category.Bytes, err = addChecked(category.Bytes, size)
		if err != nil {
			return Snapshot{}, err
		}
		category.BlobCount, err = addChecked(category.BlobCount, 1)
		if err != nil {
			return Snapshot{}, err
		}
		if isProtected {
			totals.ProtectedBytes, err = addChecked(totals.ProtectedBytes, size)
		} else {
			totals.UnreferencedBytes, err = addChecked(totals.UnreferencedBytes, size)
		}
		if err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{Totals: totals, Categories: categories}, validateTotals(totals, categories)
}

func validateTotals(totals Totals, categories []Category) error {
	protectedAndUnreferenced, err := addChecked(totals.ProtectedBytes, totals.UnreferencedBytes)
	if err != nil || protectedAndUnreferenced != totals.RegisteredBytes {
		return fmt.Errorf("storageanalysis/service: %w", errTotalInvariant)
	}
	var categoryBytes int64
	var categoryCount int64
	for _, category := range categories {
		categoryBytes, err = addChecked(categoryBytes, category.Bytes)
		if err != nil {
			return err
		}
		categoryCount, err = addChecked(categoryCount, category.BlobCount)
		if err != nil {
			return err
		}
	}
	if categoryBytes != totals.RegisteredBytes || categoryCount != totals.BlobCount {
		return fmt.Errorf("storageanalysis/service: %w", errCategoryInvariant)
	}
	return nil
}
