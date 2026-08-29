package storageanalysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
)

var (
	errProtectedBlobMissing = errors.New("STORAGE_ANALYSIS_PROTECTED_BLOB_MISSING")
	errTotalInvariant       = errors.New("STORAGE_ANALYSIS_TOTAL_INVARIANT_FAILED")
	errCategoryInvariant    = errors.New("STORAGE_ANALYSIS_CATEGORY_INVARIANT_FAILED")
	errCandidateBlobMissing = errors.New("STORAGE_ANALYSIS_CANDIDATE_BLOB_MISSING")
	errSaveBlobMissing      = errors.New("STORAGE_ANALYSIS_SAVE_BLOB_MISSING")
)

type Service struct {
	database *sql.DB
	now      func() time.Time
}

type blob struct {
	id   string
	size int64
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func (service *Service) Analyze(ctx context.Context) (Snapshot, error) {
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, fmt.Errorf("storageanalysis/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	edges, err := blobregistry.Load()
	if err != nil {
		return Snapshot{}, fmt.Errorf("storageanalysis/service: %w", err)
	}
	if err := validateReferenceCoverage(edges); err != nil {
		return Snapshot{}, err
	}
	protected, err := blobregistry.ProtectiveSet(ctx, transaction)
	if err != nil {
		return Snapshot{}, fmt.Errorf("storageanalysis/service: %w", err)
	}
	usageByID, err := loadUsage(ctx, transaction, edges)
	if err != nil {
		return Snapshot{}, err
	}
	if err := propagateArchiveUsage(ctx, transaction, protected, usageByID); err != nil {
		return Snapshot{}, err
	}
	blobs, err := loadBlobs(ctx, transaction)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := aggregate(blobs, protected, usageByID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Details, err = loadDetails(ctx, transaction, blobs)
	if err != nil {
		return Snapshot{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Snapshot{}, fmt.Errorf("storageanalysis/service: commit: %w", err)
	}
	snapshot.Scope = Scope
	snapshot.GeneratedAtMS = service.now().UnixMilli()
	snapshot.Excluded = append([]string(nil), Excluded[:]...)
	for _, category := range snapshot.Categories {
		if category.Code == CategoryOtherReferenced && category.BlobCount > 0 {
			slog.WarnContext(ctx, "storage analysis found uncategorized protected blobs",
				"category", category.Code, "blob_count", category.BlobCount, "bytes", category.Bytes)
		}
	}
	return snapshot, nil
}

func loadBlobs(ctx context.Context, transaction *sql.Tx) (map[string]int64, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT id, size_bytes FROM blobs ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("storageanalysis/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := map[string]int64{}
	for rows.Next() {
		var item blob
		if err := rows.Scan(&item.id, &item.size); err != nil {
			return nil, fmt.Errorf("storageanalysis/service: %w", err)
		}
		result[item.id] = item.size
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storageanalysis/service: %w", err)
	}
	return result, nil
}

func aggregate(
	blobs map[string]int64,
	protected map[string]struct{},
	usageByID map[string]usage,
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

func loadDetails(ctx context.Context, transaction *sql.Tx, blobs map[string]int64) (Details, error) {
	var result Details
	if err := transaction.QueryRowContext(ctx, `
SELECT
  COUNT(*) FILTER (WHERE deleted_at_ms IS NULL),
  COUNT(*) FILTER (WHERE deleted_at_ms IS NOT NULL)
FROM save_states`).Scan(&result.SaveStates.ActiveCount, &result.SaveStates.DeletedCount); err != nil {
		return Details{}, fmt.Errorf("storageanalysis/service: %w", err)
	}
	var err error
	result.SaveStates.StateReferenceBytes, err = distinctReferenceBytes(
		ctx, transaction, `SELECT DISTINCT payload_blob_id FROM save_states`, blobs,
	)
	if err != nil {
		return Details{}, err
	}
	result.SaveStates.ScreenshotReferenceBytes, err = distinctReferenceBytes(
		ctx, transaction, `SELECT DISTINCT screenshot_blob_id FROM save_states WHERE screenshot_blob_id IS NOT NULL`, blobs,
	)
	if err != nil {
		return Details{}, err
	}
	rows, err := transaction.QueryContext(ctx, `SELECT DISTINCT blob_id FROM blob_gc_candidates`)
	if err != nil {
		return Details{}, fmt.Errorf("storageanalysis/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Details{}, fmt.Errorf("storageanalysis/service: %w", err)
		}
		size, ok := blobs[id]
		if !ok {
			return Details{}, fmt.Errorf("storageanalysis/service: %w", errCandidateBlobMissing)
		}
		result.CleanupCandidates.BlobCount, err = addChecked(result.CleanupCandidates.BlobCount, 1)
		if err != nil {
			return Details{}, err
		}
		result.CleanupCandidates.Bytes, err = addChecked(result.CleanupCandidates.Bytes, size)
		if err != nil {
			return Details{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return Details{}, fmt.Errorf("storageanalysis/service: %w", err)
	}
	return result, nil
}

func distinctReferenceBytes(
	ctx context.Context,
	transaction *sql.Tx,
	query string,
	blobs map[string]int64,
) (int64, error) {
	rows, err := transaction.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("storageanalysis/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var total int64
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("storageanalysis/service: %w", err)
		}
		size, ok := blobs[id]
		if !ok {
			return 0, fmt.Errorf("storageanalysis/service: %w", errSaveBlobMissing)
		}
		total, err = addChecked(total, size)
		if err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("storageanalysis/service: %w", err)
	}
	return total, nil
}
