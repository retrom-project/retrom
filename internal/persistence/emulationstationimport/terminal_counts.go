package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

func LoadTerminalItemCounts(
	ctx context.Context,
	transaction dbexec.Executor,
	importID string,
) (application.TerminalItemCounts, error) {
	var counts application.TerminalItemCounts
	err := transaction.QueryRowContext(ctx, `
SELECT
 count(*) FILTER(WHERE execution_state='SKIPPED_MAPPING'),
 count(*) FILTER(WHERE execution_state='REVIEW_PENDING'),
 count(*) FILTER(WHERE execution_state='PUBLISHED'),
 count(*) FILTER(WHERE execution_state='REVIEW_DISCARDED'),
 count(*) FILTER(WHERE execution_state='SKIPPED_EXISTING'),
 count(*) FILTER(WHERE execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),
 count(*) FILTER(WHERE execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
 count(*) FILTER(WHERE execution_state='CANCELLED')
FROM emulationstation_import_items
WHERE import_id=?`, importID).Scan(
		&counts.SkippedMapping,
		&counts.ReviewPending,
		&counts.Published,
		&counts.ReviewDiscarded,
		&counts.Existing,
		&counts.Blocked,
		&counts.Failed,
		&counts.Cancelled,
	)
	if err != nil {
		return application.TerminalItemCounts{}, fmt.Errorf("emulationstationimport/read terminal counts: %w", err)
	}
	return counts, nil
}

func terminalCountValues(counts application.TerminalItemCounts) []any {
	return []any{
		counts.SkippedMapping,
		counts.ReviewPending,
		counts.Published,
		counts.ReviewDiscarded,
		counts.Existing,
		counts.Blocked,
		counts.Failed,
		counts.Cancelled,
	}
}
