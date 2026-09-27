// Package blobrefs maintains Blob protection at explicit transactional write boundaries.
package blobrefs

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
)

var ErrCount = errors.New("BLOB_REFERENCE_COUNT_CONFLICT")

// Delta counts reference edges, including repeated references to the same Blob.
type Delta map[string]int64

func (delta Delta) Add(id string, amount int64) error {
	if id == "" || amount == 0 {
		return nil
	}
	previous := delta[id]
	if amount > 0 && previous > math.MaxInt64-amount || amount < 0 && previous < math.MinInt64-amount {
		return ErrCount
	}
	delta[id] = previous + amount
	return nil
}

type Changes struct{ Zero, Protected []string }

// Apply updates counts and archive-derived protection in the caller's transaction.
// The returned IDs include indirect members reaching zero, for atomic GC staging.
func Apply(ctx context.Context, tx dbapi.Executor, delta Delta) (Changes, error) {
	pending := make(Delta, len(delta))
	for id, amount := range delta {
		pending[id] = amount
	}
	touched := make(map[string]bool)
	for len(pending) != 0 {
		next, err := applyBatch(ctx, tx, pending, touched)
		if err != nil {
			return Changes{}, err
		}
		pending = next
	}
	return finalCounts(ctx, tx, touched)
}

func applyBatch(ctx context.Context, tx dbapi.Executor, pending Delta, touched map[string]bool) (Delta, error) {
	next := make(Delta)
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		amount := pending[id]
		if amount == 0 || id == "" {
			continue
		}
		before, after, err := change(ctx, tx, id, amount)
		if err != nil {
			return nil, err
		}
		touched[id] = true
		if before == 0 && after > 0 || before > 0 && after == 0 {
			sign := int64(1)
			if after == 0 {
				sign = -1
			}
			if err := members(ctx, tx, id, sign, next); err != nil {
				return nil, err
			}
		}
	}

	return next, nil
}

func change(ctx context.Context, tx dbapi.Executor, id string, amount int64) (int64, int64, error) {
	var before int64
	if err := dbapi.QueryRowContext(ctx, tx, `SELECT ref_count FROM blobs WHERE id=?`, id).Scan(&before); err != nil {
		return 0, 0, fmt.Errorf("read Blob count: %w", err)
	}
	if before < 0 || amount > 0 && before > math.MaxInt64-amount || amount < 0 && amount < -before {
		return 0, 0, fmt.Errorf("%w: Blob %s count %d delta %d", ErrCount, id, before, amount)
	}
	after := before + amount
	result, err := tx.ExecContext(ctx, `UPDATE blobs SET ref_count=? WHERE id=? AND ref_count=?`, after, id, before)
	if err != nil {
		return 0, 0, fmt.Errorf("update Blob count: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, 0, fmt.Errorf("confirm Blob count: %w", err)
	}
	if count != 1 {
		return 0, 0, ErrCount
	}
	return before, after, nil
}

func members(ctx context.Context, tx dbapi.Executor, id string, sign int64, delta Delta) error {
	rows, err := tx.QueryContext(ctx, `SELECT materialized_blob_id,count(*) FROM archive_entries
WHERE archive_blob_id=? AND materialized_blob_id IS NOT NULL AND materialized_blob_id<>archive_blob_id
GROUP BY materialized_blob_id`, id)
	if err != nil {
		return fmt.Errorf("read protected archive members: %w", err)
	}
	defer func() { cleanup.Error("close archive members", rows.Close()) }()
	for rows.Next() {
		var member string
		var count int64
		if err := rows.Scan(&member, &count); err != nil {
			return fmt.Errorf("read archive membership: %w", err)
		}
		if err := delta.Add(member, sign*count); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate archive membership: %w", err)
	}
	return nil
}

func finalCounts(ctx context.Context, tx dbapi.Executor, touched map[string]bool) (Changes, error) {
	var result Changes
	for id := range touched {
		var count int64
		if err := dbapi.QueryRowContext(ctx, tx, `SELECT ref_count FROM blobs WHERE id=?`, id).Scan(&count); err != nil {
			return Changes{}, fmt.Errorf("read final Blob count: %w", err)
		}
		if count == 0 {
			result.Zero = append(result.Zero, id)
		} else {
			result.Protected = append(result.Protected, id)
		}
	}
	sort.Strings(result.Zero)
	sort.Strings(result.Protected)
	return result, nil
}
