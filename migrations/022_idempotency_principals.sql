CREATE TABLE idempotency_records_v22 (
  principal_id TEXT NOT NULL CHECK(length(principal_id) BETWEEN 1 AND 128),
  operation_id TEXT NOT NULL,
  key TEXT NOT NULL,
  request_digest TEXT NOT NULL CHECK(length(request_digest) = 64),
  http_status INTEGER NOT NULL CHECK(http_status BETWEEN 100 AND 599),
  response_headers_json TEXT NOT NULL,
  response_body BLOB NOT NULL CHECK(length(response_body) <= 1048576),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms >= created_at_ms),
  PRIMARY KEY(principal_id, operation_id, key)
);

-- Released account-enabled databases are green-field and therefore have no
-- pre-authentication business records. Keep the copy path deterministic for
-- short-lived development databases created by the preceding migration.
INSERT INTO idempotency_records_v22(
  principal_id,operation_id,key,request_digest,http_status,response_headers_json,
  response_body,created_at_ms,expires_at_ms
)
SELECT 'SYSTEM',operation_id,key,request_digest,http_status,response_headers_json,
       response_body,created_at_ms,expires_at_ms
FROM idempotency_records;

DROP TABLE idempotency_records;
ALTER TABLE idempotency_records_v22 RENAME TO idempotency_records;
