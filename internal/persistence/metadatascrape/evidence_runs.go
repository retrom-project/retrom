package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/service/metadatascrape"
)

func (records *EvidenceQueries) ReviewRuns(ctx context.Context, itemID string) ([]application.ReviewRun, error) {
	rows, err := records.executor.QueryContext(ctx, `
WITH evidence_counts AS (
  SELECT scrape_run_id,
  COUNT(*) AS evidence_count
  FROM content_hash_evidence
  GROUP BY scrape_run_id
), candidate_counts AS (
  SELECT scrape_run_id,
  COUNT(*) AS candidate_count
  FROM scrape_candidates
  GROUP BY scrape_run_id
), outcome_counts AS (
  SELECT a.scrape_run_id,
  COUNT(*) AS attempt_count,
  SUM(CASE WHEN p.outcome='HIT' THEN 1 ELSE 0 END) AS hit,
  SUM(CASE WHEN p.outcome='MISS' THEN 1 ELSE 0 END) AS miss,
  SUM(CASE WHEN p.outcome='RATE_LIMITED' THEN 1 ELSE 0 END) AS rate_limited,
  SUM(CASE WHEN p.outcome='TIMEOUT' THEN 1 ELSE 0 END) AS timeout,
  SUM(CASE WHEN p.outcome='INVALID_RESPONSE' THEN 1 ELSE 0 END) AS invalid_response,
  SUM(CASE WHEN p.outcome='NETWORK_ERROR' THEN 1 ELSE 0 END) AS network_error
  FROM metadata_scrape_query_attempts a
  JOIN metadata_provider_responses p ON p.id=a.provider_response_id
  GROUP BY a.scrape_run_id
)
SELECT r.id,
r.job_id,
r.provider,
r.state,
j.state,
r.created_at_ms,
r.completed_at_ms,
r.error_code,
COALESCE(e.evidence_count,0),
COALESCE(o.attempt_count,0),
COALESCE(c.candidate_count,0),
COALESCE(o.hit,0),
COALESCE(o.miss,0),
COALESCE(o.rate_limited,0),
COALESCE(o.timeout,0),
COALESCE(o.invalid_response,0),
COALESCE(o.network_error,0)
FROM metadata_scrape_runs r
JOIN jobs j ON j.id=r.job_id
LEFT JOIN evidence_counts e ON e.scrape_run_id=r.id
LEFT JOIN candidate_counts c ON c.scrape_run_id=r.id
LEFT JOIN outcome_counts o ON o.scrape_run_id=r.id
WHERE r.import_item_id=?
ORDER BY r.created_at_ms DESC,
r.id DESC
LIMIT 10
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review runs: %w", err)
	}
	defer func() { cleanup.Error("close review runs", rows.Close()) }()
	result := make([]application.ReviewRun, 0)
	for rows.Next() {
		var row application.ReviewRun
		if err := rows.Scan(&row.ID,
			&row.JobID,
			&row.Provider,
			&row.State,
			&row.JobState,
			&row.CreatedAtMS,
			&row.CompletedAtMS,
			&row.ErrorCode,
			&row.EvidenceCount,
			&row.AttemptCount,
			&row.CandidateCount,
			&row.Outcomes.Hit,
			&row.Outcomes.Miss,
			&row.Outcomes.RateLimited,
			&row.Outcomes.Timeout,
			&row.Outcomes.InvalidResponse,
			&row.Outcomes.NetworkError); err != nil {
			return nil, fmt.Errorf("scan review run: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review runs: %w", err)
	}
	return result, nil
}
