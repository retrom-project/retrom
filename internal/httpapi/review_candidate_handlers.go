package httpapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"retrom/internal/cleanup"
)

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanReviewScrapeRun(scanner rowScanner) (map[string]any, error) {
	var runID, jobID, provider, state, jobState string
	var createdAtMS, evidenceCount, attemptCount, candidateCount int64
	var completedAtMS sql.NullInt64
	var errorCode sql.NullString
	var hit, miss, rateLimited, timeout, invalidResponse, networkError int64
	if err := scanner.Scan(
		&runID,
		&jobID,
		&provider,
		&state,
		&jobState,
		&createdAtMS,
		&completedAtMS,
		&errorCode,
		&evidenceCount,
		&attemptCount,
		&candidateCount,
		&hit,
		&miss,
		&rateLimited,
		&timeout,
		&invalidResponse,
		&networkError,
	); err != nil {
		return nil, fmt.Errorf("scan review scrape run: %w", err)
	}
	return map[string]any{
		"scrapeRunId":    runID,
		"jobId":          jobID,
		"provider":       provider,
		"state":          state,
		"jobState":       jobState,
		"createdAtMs":    createdAtMS,
		"completedAtMs":  nullableInt64(completedAtMS),
		"errorCode":      nullableString(errorCode),
		"evidenceCount":  evidenceCount,
		"attemptCount":   attemptCount,
		"candidateCount": candidateCount,
		"outcomes": map[string]any{
			"hit":             hit,
			"miss":            miss,
			"rateLimited":     rateLimited,
			"timeout":         timeout,
			"invalidResponse": invalidResponse,
			"networkError":    networkError,
		},
	}, nil
}

func (server *Server) reviewCandidates(request *http.Request, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT c.id,
c.scrape_run_id,
c.provider_game_id,
c.normalized_metadata_json,
c.evidence_json,
c.created_at_ms
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.import_item_id=?
AND r.state='COMPLETED'
ORDER BY r.created_at_ms DESC,
c.created_at_ms,
c.id
`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("httpapi/server: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type candidateRow struct {
		id, runID, providerGameID, metadataJSON, evidenceJSON string
		createdAtMS                                           int64
	}
	records := make([]candidateRow, 0)
	for rows.Next() {
		var record candidateRow
		if err := rows.Scan(
			&record.id,
			&record.runID,
			&record.providerGameID,
			&record.metadataJSON,
			&record.evidenceJSON,
			&record.createdAtMS,
		); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review candidates: %w", err)
	}
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var metadataValue, evidenceValue any
		if err := json.Unmarshal([]byte(record.metadataJSON), &metadataValue); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		if err := json.Unmarshal([]byte(record.evidenceJSON), &evidenceValue); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		assets, err := server.reviewCandidateAssets(request, record.id)
		if err != nil {
			return nil, err
		}
		result = append(
			result,
			map[string]any{
				"candidateId":    record.id,
				"scrapeRunId":    record.runID,
				"providerGameId": record.providerGameID,
				"metadata":       metadataValue,
				"evidence":       evidenceValue,
				"assets":         assets,
				"createdAtMs":    record.createdAtMS,
			},
		)
	}
	return result, nil
}

func (server *Server) reviewCandidateAssets(request *http.Request, candidateID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT id,
provider_asset_id,
kind_hint,
ordinal,
status,
width_px,
height_px,
media_type,
error_code
FROM scrape_candidate_assets
WHERE scrape_candidate_id=?
ORDER BY kind_hint,
ordinal,
id
`,
		candidateID,
	)
	if err != nil {
		return nil, fmt.Errorf("httpapi/server: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]map[string]any, 0)
	for rows.Next() {
		var id, providerAssetID, kind, status string
		var ordinal int64
		var width, height sql.NullInt64
		var mediaType, errorCode sql.NullString
		if err := rows.Scan(
			&id,
			&providerAssetID,
			&kind,
			&ordinal,
			&status,
			&width,
			&height,
			&mediaType,
			&errorCode,
		); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		assets = append(
			assets,
			map[string]any{
				"candidateAssetId": id,
				"providerAssetId":  providerAssetID,
				"kind":             kind,
				"ordinal":          ordinal,
				"status":           status,
				"widthPx":          nullableInt64(width),
				"heightPx":         nullableInt64(height),
				"mediaType":        nullableString(mediaType),
				"errorCode":        nullableString(errorCode),
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi/server: %w", err)
	}
	return assets, nil
}
