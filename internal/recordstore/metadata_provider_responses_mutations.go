package recordstore

import (
	"context"
	"database/sql"
)

func UpdateMetadataProviderResponses(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"metadata_provider_responses",
		"id,expires_at_ms,fetched_at_ms,http_status,outcome,provider,raw_payload_state,"+
			"request_digest",
		MetadataProviderResponsesUpdateRule,
	)
}

const MetadataProviderResponsesUpdateRule = `
WITH previous(id,expires_at_ms,fetched_at_ms,http_status,outcome,provider,raw_payload_state,
request_digest) AS (VALUES(?,?,?,?,?,?,?,?))
SELECT CASE
-- provider_responses_immutable_update
WHEN (NOT (
  previous.raw_payload_state='RETAINED' AND candidate.raw_payload_state='RELEASED'
  AND candidate.raw_response_blob_id IS NULL AND candidate.raw_payload_released_at_ms IS NOT NULL
  AND candidate.id=previous.id AND candidate.provider=previous.provider AND
candidate.request_digest=previous.request_digest
  AND candidate.http_status IS previous.http_status AND candidate.outcome=previous.outcome
  AND candidate.fetched_at_ms=previous.fetched_at_ms AND candidate.expires_at_ms=previous.expires_at_ms
)) THEN 'immutable'
ELSE '' END
FROM metadata_provider_responses candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteMetadataProviderResponses(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"metadata_provider_responses",
		"id",
		MetadataProviderResponsesDeleteRule,
	)
}

const MetadataProviderResponsesDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- provider_responses_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
