package filedeletion

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/persistence/jobrecord"
	application "retrom/internal/service/cleanupjobs"
)

const deletionSQL = `SELECT blob.id,blob.sha256,blob.size_bytes,candidate.blob_id IS NOT NULL,
 COALESCE(candidate.retired_at_ms,0),COALESCE(candidate.scheduled_at_ms,0),
 COALESCE(candidate.attempt_count,0),blob.retired_at_ms IS NULL,` + jobrecord.Columns + ` FROM stored_files blob
 LEFT JOIN file_deletions candidate ON candidate.blob_id=blob.id
 LEFT JOIN jobs job ON job.id=candidate.deletion_job_id
 LEFT JOIN job_input_snapshots input ON input.job_id=job.id AND input.execution_no=job.execution_no `

func (records deletionRecords) Page(
	ctx context.Context,
	after string,
	limit int,
) ([]application.DeletionFile, error) {
	return records.read(
		ctx,
		deletionSQL+` WHERE blob.retired_at_ms IS NOT NULL AND candidate.blob_id IS NULL AND blob.id>? ORDER
BY blob.id LIMIT ?`,
		after,
		limit,
	)
}

func (records deletionRecords) Selected(
	ctx context.Context,
	ids []string,
) ([]application.DeletionFile, error) {
	if len(ids) == 0 {
		return []application.DeletionFile{}, nil
	}
	marks, args := deletionIDParameters(ids)
	return records.read(ctx, deletionSQL+` WHERE blob.id IN (`+marks+`) ORDER BY blob.id`, args...)
}

func (records deletionRecords) Candidates(ctx context.Context) ([]application.DeletionFile, error) {
	return records.read(ctx, deletionSQL+` WHERE candidate.blob_id IS NOT NULL ORDER BY blob.id`)
}

func (records deletionRecords) read(
	ctx context.Context,
	query string,
	args ...any,
) ([]application.DeletionFile, error) {
	rows, err := records.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query file deletion facts: %w", err)
	}
	defer func() { cleanup.Error("close file deletion facts", rows.Close()) }()
	facts := make([]application.DeletionFile, 0)
	for rows.Next() {
		var blob application.DeletionFile
		work, _, err := jobrecord.Read(deletionScanner{row: rows, blob: &blob})
		if err != nil {
			return nil, fmt.Errorf("read file deletion facts: %w", err)
		}
		blob.Candidate.Work = work
		facts = append(facts, blob)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file deletion facts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close file deletion facts: %w", err)
	}
	return facts, nil
}

type deletionScanner struct {
	row  jobrecord.Scanner
	blob *application.DeletionFile
}

func (scanner deletionScanner) Scan(destinations ...any) error {
	blob := scanner.blob
	args := append(make([]any, 0, 8+len(destinations)),
		&blob.ID, &blob.Digest, &blob.SizeBytes, &blob.HasCandidate,
		&blob.Candidate.RetiredMS, &blob.Candidate.ScheduledMS, &blob.Candidate.Attempt,
		&blob.Retained,
	)
	if err := scanner.row.Scan(append(args, destinations...)...); err != nil {
		return fmt.Errorf("scan file deletion facts: %w", err)
	}
	return nil
}

func deletionIDParameters(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
