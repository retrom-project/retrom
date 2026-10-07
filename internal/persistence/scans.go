package persistence

import (
	"context"

	"retrom/internal/model"
)

const scanJSON = `jsonb_build_object('id',id,'scanType',scan_type,'status',status,'totalCount',COALESCE(total_count,0),
 'totalKnown',total_count IS NOT NULL,'processedCount',processed_count,'importedCount',imported_count,
 'skippedCount',
skipped_count,
'failedCount',
failed_count,
'createdAtMs',
created_at_ms,
'updatedAtMs',
updated_at_ms,
'error',
error_summary)`

func (r *Repository) CreateScan(ctx context.Context, scan model.Scan, userID string) error {
	return r.Execute(ctx,
		`INSERT INTO scan_progress_tab(id,
scan_type,
status,
total_count,
processed_count,
imported_count,
skipped_count,
failed_count,
created_by_user_id,
created_at_ms,
updated_at_ms)
 VALUES($1,$2,'running',NULL,0,0,0,0,$3,$4,$4)`, scan.ID, scan.ScanType, userID, scan.CreatedAtMs)
}

func (r *Repository) Scans(ctx context.Context, q model.Query) (model.Page[model.Scan], error) {
	page := model.Page[model.Scan]{Offset: q.Offset, Limit: q.Limit}
	var err error
	page.Items,
		err = listJSON[model.Scan](ctx,
		r,
		"SELECT "+scanJSON+" FROM scan_progress_tab ORDER BY created_at_ms DESC,id LIMIT $1 OFFSET $2",
		q.Limit,
		q.Offset)
	if err != nil {
		return page, err
	}
	page.Total, err = r.Count(ctx, "SELECT count(*) FROM scan_progress_tab")
	return page, err
}

func (r *Repository) Scan(ctx context.Context, id string) (model.Scan, error) {
	return readJSON[model.Scan](ctx, r, "SELECT "+scanJSON+" FROM scan_progress_tab WHERE id=$1", id)
}

func (r *Repository) UpdateScan(ctx context.Context, scan model.Scan) error {
	var total *int64
	if scan.TotalKnown {
		total = &scan.TotalCount
	}
	return r.Execute(ctx,
		`UPDATE scan_progress_tab SET status=$2,
total_count=$3,
processed_count=$4,
imported_count=$5,
skipped_count=$6,
failed_count=$7,
error_summary=$8,
updated_at_ms=$9,
finished_at_ms=CASE WHEN $2='running' THEN NULL ELSE $9::bigint END WHERE id=$1`,
		scan.ID,
		scan.Status,
		total,
		scan.ProcessedCount,
		scan.ImportedCount,
		scan.SkippedCount,
		scan.FailedCount,
		scan.Error,
		scan.UpdatedAtMs)
}

func (r *Repository) InterruptScans(ctx context.Context, now int64) error {
	return r.Execute(ctx,
		`UPDATE scan_progress_tab SET status='interrupted',
finished_at_ms=$1,
updated_at_ms=$1 WHERE status='running'`,
		now)
}

func (r *Repository) ImportExists(ctx context.Context, table, id string) (bool, error) {
	if !model.OneOf(table, "game_tab", "bios_file_tab") {
		return false, model.ErrInvalid
	}
	count, err := r.Count(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id)
	return count == 1, err
}

func (r *Repository) StopScan(ctx context.Context, id, status, code string, now int64) error {
	return r.Execute(ctx,
		`UPDATE scan_progress_tab SET status=$2,error_summary=$3,updated_at_ms=$4,finished_at_ms=$4 WHERE id=$1`,
		id, status, code, now)
}
