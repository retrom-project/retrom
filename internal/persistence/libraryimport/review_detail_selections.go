package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

func (records ReviewDrafts) ScreenshotIDs(ctx context.Context, itemID string) ([]string, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT s.candidate_asset_id
FROM review_draft_screenshot_assets s
JOIN review_drafts d ON d.id=s.review_draft_id
WHERE d.import_item_id=?
ORDER BY s.ordinal
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review selected screenshots: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan selected screenshot: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate selected screenshots: %w", err)
	}
	return result, nil
}

func (records ReviewDrafts) DOSEntries(ctx context.Context, itemID string) ([]application.ReviewDOSEntry, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM import_item_dos_entries
WHERE import_item_id=?
ORDER BY rank,normalized_path
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review DOS entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]application.ReviewDOSEntry, 0)
	for rows.Next() {
		var row application.ReviewDOSEntry
		if err := scanReviewDOSEntry(rows, &row); err != nil {
			return nil, fmt.Errorf("scan review DOS entry: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review DOS entries: %w", err)
	}
	return result, nil
}

func scanReviewDOSEntry(scanner dbexec.Scanner, row *application.ReviewDOSEntry) error {
	if err := scanner.Scan(
		&row.Path, &row.OriginalPath, &row.Kind, &row.Rank, &row.Enabled, &row.DirectLaunchSafe,
	); err != nil {
		return fmt.Errorf("scan review DOS entry columns: %w", err)
	}
	return nil
}
