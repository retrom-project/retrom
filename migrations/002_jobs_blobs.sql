CREATE TABLE blobs (
  id TEXT PRIMARY KEY,
  sha256 TEXT NOT NULL UNIQUE CHECK(length(sha256) = 64 AND sha256 = lower(sha256)),
  size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
  md5 TEXT NOT NULL CHECK(length(md5) = 32 AND md5 = lower(md5)),
  sha1 TEXT NOT NULL CHECK(length(sha1) = 40 AND sha1 = lower(sha1)),
  crc32 TEXT NOT NULL CHECK(length(crc32) = 8 AND crc32 = lower(crc32)),
  media_type TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0)
);

CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE','METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP')),
  dedupe_key TEXT NOT NULL CHECK(length(dedupe_key) = 64),
  execution_no INTEGER NOT NULL CHECK(execution_no >= 1),
  payload_json TEXT NOT NULL,
  cancellable INTEGER NOT NULL CHECK(cancellable IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','CANCEL_REQUESTED','SUCCEEDED','FAILED','CANCELLED')),
  attempt_count INTEGER NOT NULL CHECK(attempt_count >= 0),
  max_attempts INTEGER NOT NULL CHECK(max_attempts BETWEEN 1 AND 4),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  available_at_ms INTEGER NOT NULL CHECK(available_at_ms >= 0),
  execution_started_at_ms INTEGER,
  execution_deadline_at_ms INTEGER,
  leased_until_ms INTEGER,
  heartbeat_at_ms INTEGER,
  finished_at_ms INTEGER,
  worker_id TEXT,
  error_code TEXT,
  error_retryable INTEGER CHECK(error_retryable IS NULL OR error_retryable IN (0,1)),
  cancel_requested_at_ms INTEGER,
  cancel_reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  UNIQUE(kind, dedupe_key),
  CHECK((state IN ('SUCCEEDED','FAILED','CANCELLED')) = (finished_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED')) = (cancel_requested_at_ms IS NOT NULL))
);
CREATE INDEX jobs_claim ON jobs(state, available_at_ms);
CREATE INDEX jobs_scope ON jobs(scope_type, scope_id);

CREATE TABLE job_input_snapshots (
  job_id TEXT NOT NULL REFERENCES jobs(id),
  execution_no INTEGER NOT NULL CHECK(execution_no >= 1),
  input_json TEXT NOT NULL,
  input_digest TEXT NOT NULL CHECK(length(input_digest) = 64),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  PRIMARY KEY(job_id, execution_no)
);

CREATE TABLE job_events (
  id INTEGER PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id),
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  event_type TEXT NOT NULL CHECK(event_type IN ('QUEUED','STARTED','PROGRESS','RETRY_SCHEDULED','CANCEL_REQUESTED','MANUAL_RETRY','SUCCEEDED','FAILED','CANCELLED')),
  data_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0)
);
CREATE INDEX job_events_scope ON job_events(scope_type, scope_id, id);
CREATE INDEX job_events_job ON job_events(job_id, id);

CREATE TABLE idempotency_records (
  operation_id TEXT NOT NULL,
  key TEXT NOT NULL,
  request_digest TEXT NOT NULL CHECK(length(request_digest) = 64),
  http_status INTEGER NOT NULL CHECK(http_status BETWEEN 100 AND 599),
  response_headers_json TEXT NOT NULL,
  response_body BLOB NOT NULL CHECK(length(response_body) <= 1048576),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms >= created_at_ms),
  PRIMARY KEY(operation_id, key)
);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL CHECK(actor = 'local'),
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  before_json TEXT,
  after_json TEXT,
  diff_json TEXT,
  request_id TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0)
);

CREATE TABLE blob_gc_candidates (
  blob_id TEXT PRIMARY KEY REFERENCES blobs(id),
  first_unreferenced_at_ms INTEGER NOT NULL CHECK(first_unreferenced_at_ms >= 0),
  scheduled_at_ms INTEGER NOT NULL CHECK(scheduled_at_ms >= first_unreferenced_at_ms),
  deleted_at_ms INTEGER,
  last_failed_at_ms INTEGER,
  error_code TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0)
);

CREATE TRIGGER job_input_snapshots_immutable_update BEFORE UPDATE ON job_input_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER job_input_snapshots_immutable_delete BEFORE DELETE ON job_input_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER audit_events_immutable_update BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER audit_events_immutable_delete BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
