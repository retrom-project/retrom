package payloadrelease

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/blobregistry"
)

const gcSQL = `SELECT blob.id,blob.sha256,blob.size_bytes,candidate.blob_id IS NOT NULL,
 COALESCE(candidate.first_unreferenced_at_ms,0),COALESCE(candidate.scheduled_at_ms,0),
 COALESCE(candidate.attempt_count,0),` + workColumns + ` FROM blobs blob
 LEFT JOIN blob_gc_candidates candidate ON candidate.blob_id=blob.id
 LEFT JOIN jobs job ON job.id=candidate.gc_job_id
 LEFT JOIN job_input_snapshots input ON input.job_id=job.id AND input.execution_no=job.execution_no `

func (records gcRecords) Page(ctx context.Context, after string, limit int) ([]application.GCBlob, error) {
	return records.read(ctx, gcSQL+` WHERE blob.id>? ORDER BY blob.id LIMIT ?`, after, limit)
}

func (records gcRecords) Selected(ctx context.Context, ids []string) ([]application.GCBlob, error) {
	if len(ids) == 0 {
		return []application.GCBlob{}, nil
	}
	marks, args := gcIDParameters(ids)
	return records.read(ctx, gcSQL+` WHERE blob.id IN (`+marks+`) ORDER BY blob.id`, args...)
}

func (records gcRecords) Candidates(ctx context.Context) ([]application.GCBlob, error) {
	return records.read(ctx, gcSQL+` WHERE candidate.blob_id IS NOT NULL ORDER BY blob.id`)
}

func (records gcRecords) read(ctx context.Context, query string, args ...any) ([]application.GCBlob, error) {
	rows, err := records.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query GC facts: %w", err)
	}
	defer func() { cleanup.Error("close GC facts", rows.Close()) }()
	facts := make([]application.GCBlob, 0)
	for rows.Next() {
		var blob application.GCBlob
		work, _, err := readWork(gcScanner{row: rows, blob: &blob})
		if err != nil {
			return nil, fmt.Errorf("read GC facts: %w", err)
		}
		blob.Candidate.Work = work
		facts = append(facts, blob)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate GC facts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close GC facts: %w", err)
	}
	if len(facts) == 0 {
		return facts, nil
	}
	protected, err := blobregistry.ProtectiveSet(ctx, records.executor)
	if err != nil {
		return nil, fmt.Errorf("read GC protection: %w", err)
	}
	for index := range facts {
		_, facts[index].Protected = protected[facts[index].ID]
	}
	return facts, nil
}

type gcScanner struct {
	row  workScanner
	blob *application.GCBlob
}

func (scanner gcScanner) Scan(destinations ...any) error {
	blob := scanner.blob
	args := append(make([]any, 0, 7+len(destinations)),
		&blob.ID, &blob.Digest, &blob.SizeBytes, &blob.HasCandidate,
		&blob.Candidate.FirstUnreferencedMS, &blob.Candidate.ScheduledMS, &blob.Candidate.Attempt,
	)
	if err := scanner.row.Scan(append(args, destinations...)...); err != nil {
		return fmt.Errorf("scan GC facts: %w", err)
	}
	return nil
}

func gcIDParameters(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
