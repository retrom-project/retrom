package blobgc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"retrom/internal/blobregistry"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
)

var errRetentionInvalid = errors.New("GC_RETENTION_INVALID")

type Result struct {
	Protected int `json:"protected"`
	Scheduled int `json:"scheduled"`
	Deleted   int `json:"deleted"`
	Retained  int `json:"retained"`
}

type Service struct {
	database  *sql.DB
	blobs     *blobstore.Store
	now       func() time.Time
	retention time.Duration
}

type candidate struct {
	id, digest string
	protected  bool
}

func New(database *sql.DB, blobs *blobstore.Store, now func() time.Time, retention time.Duration) (*Service, error) {
	if retention < 24*time.Hour || retention > 30*24*time.Hour {
		return nil, errRetentionInvalid
	}
	return &Service{database: database, blobs: blobs, now: now, retention: retention}, nil
}

// GC staging and deletion remain one auditable operation.
func (service *Service) RunOnce(ctx context.Context) (Result, error) {
	if err := blobregistry.ValidateSchema(ctx, service.database); err != nil {
		return Result{}, fmt.Errorf("blobgc/service: %w", err)
	}
	now := service.now().UnixMilli()
	cutoff := now - service.retention.Milliseconds()
	// SaveState rows remain protective throughout their soft-delete grace.
	if _, err := service.database.ExecContext(ctx, `
DELETE
FROM save_states
WHERE deleted_at_ms IS NOT NULL
AND deleted_at_ms<=?
AND NOT EXISTS(SELECT 1
FROM launch_sessions
WHERE save_state_id=save_states.id)
`, cutoff); err != nil {
		return Result{}, fmt.Errorf("blobgc/service: %w", err)
	}
	protected, err := protectiveSet(ctx, service.database)
	if err != nil {
		return Result{}, err
	}
	result := Result{Protected: len(protected)}
	candidates, retained, err := collectCandidates(ctx, service.database, protected)
	if err != nil {
		return Result{}, err
	}
	result.Retained += retained
	for _, item := range candidates {
		if item.protected {
			_, _ = service.database.ExecContext(ctx, `
DELETE
FROM blob_gc_candidates
WHERE blob_id=?
`, item.id)
			continue
		}
		var scheduled int64
		err := service.database.QueryRowContext(ctx, `
SELECT scheduled_at_ms
FROM blob_gc_candidates
WHERE blob_id=?
`, item.id).
			Scan(&scheduled)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := service.database.ExecContext(ctx, `
INSERT INTO blob_gc_candidates(blob_id,
first_unreferenced_at_ms,
scheduled_at_ms,
attempt_count) VALUES(?,
?,
?,
0)
`, item.id, now, now+service.retention.Milliseconds()); err != nil {
				return Result{}, fmt.Errorf("blobgc/service: %w", err)
			}
			result.Scheduled++
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("blobgc/service: %w", err)
		}
		if scheduled > now {
			result.Retained++
			continue
		}
		deleted, err := service.deleteCandidate(ctx, item.id)
		if err != nil {
			_, _ = service.database.ExecContext(
				ctx,
				`
UPDATE blob_gc_candidates
SET last_failed_at_ms=?,
error_code='GC_DELETE_FAILED',
attempt_count=attempt_count+1
WHERE blob_id=?
`,
				now,
				item.id,
			)
			continue
		}
		if !deleted {
			result.Retained++
			continue
		}
		if err := os.Remove(service.blobs.Path(item.digest)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("remove CAS object: %w", err)
		}
		result.Deleted++
	}
	return result, nil
}

func collectCandidates(
	ctx context.Context,
	database *sql.DB,
	protected map[string]struct{},
) ([]candidate, int, error) {
	rows, err := database.QueryContext(ctx, `
SELECT id,
sha256
FROM blobs
ORDER BY id
`)
	if err != nil {
		return nil, 0, fmt.Errorf("blobgc/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	candidates := make([]candidate, 0)
	retained := 0
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.digest); err != nil {
			return nil, 0, fmt.Errorf("blobgc/service: %w", err)
		}
		if _, keep := protected[item.id]; keep {
			item.protected = true
			retained++
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("blobgc/service: %w", err)
	}
	return candidates, retained, nil
}

func (service *Service) deleteCandidate(ctx context.Context, blobID string) (bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("blobgc/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	protected, err := protectiveSet(ctx, transaction)
	if err != nil {
		return false, err
	}
	if _, keep := protected[blobID]; keep {
		_, _ = transaction.ExecContext(ctx, `
DELETE
FROM blob_gc_candidates
WHERE blob_id=?
`, blobID)
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf("commit retained GC candidate: %w", err)
		}
		return false, nil
	}
	// Ownership rows do not protect their archive; remove them as a group only
	// after every composite business reference has disappeared.
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM archive_entries
WHERE archive_blob_id=?
`, blobID); err != nil {
		return false, fmt.Errorf("blobgc/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM blob_gc_candidates
WHERE blob_id=?
`, blobID); err != nil {
		return false, fmt.Errorf("blobgc/service: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
DELETE
FROM blobs
WHERE id=?
`, blobID)
	if err != nil {
		return false, fmt.Errorf("blobgc/service: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return false, nil
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit deleted GC candidate: %w", err)
	}
	return true, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func protectiveSet(ctx context.Context, database queryer) (map[string]struct{}, error) {
	edges, err := blobregistry.Load()
	if err != nil {
		return nil, fmt.Errorf("blobgc/service: %w", err)
	}
	protected := map[string]struct{}{}
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		query := `
SELECT DISTINCT
` + quote(
			edge.Column,
		) + ` FROM ` + quote(
			edge.Table,
		) + ` WHERE ` + quote(
			edge.Column,
		) + ` IS NOT NULL`
		if err := collectProtected(ctx, database, query, nil, protected); err != nil {
			return nil, err
		}
	}
	// A protected archive owns its already materialized members. Ownership is
	// deliberately one-way: an unreferenced archive does not protect itself.
	if len(protected) > 0 {
		values := make([]any, 0, len(protected))
		placeholders := make([]string, 0, len(protected))
		for id := range protected {
			values = append(values, id)
			placeholders = append(placeholders, "?")
		}
		query := `
SELECT DISTINCT materialized_blob_id
FROM archive_entries
WHERE archive_blob_id IN (
` + strings.Join(
			placeholders,
			",",
		) + `) AND materialized_blob_id IS NOT NULL`
		if err := collectProtected(ctx, database, query, values, protected); err != nil {
			return nil, err
		}
	}
	return protected, nil
}

func collectProtected(
	ctx context.Context,
	database queryer,
	query string,
	values []any,
	protected map[string]struct{},
) error {
	rows, err := database.QueryContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("blobgc/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("blobgc/service: %w", err)
		}
		protected[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan protected blob references: %w", err)
	}
	return nil
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
