-- Clean pre-release baseline: storage_jobs.

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

CREATE TABLE "jobs" (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE','SERVER_BIOS_IMPORT',
    'SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_SCAN',
      'SERVER_EMULATIONSTATION_IMPORT','REVIEW_BULK_APPROVE','PAYLOAD_RELEASE',
      'RUNTIME_ASSET_PACK_VALIDATE'
  )),
  dedupe_key TEXT NOT NULL CHECK(length(dedupe_key)=64),
  execution_no INTEGER NOT NULL CHECK(execution_no>=1),
  payload_json TEXT NOT NULL,
  cancellable INTEGER NOT NULL CHECK(cancellable IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','CANCEL_REQUESTED','SUCCEEDED','FAILED','CANCELLED')),
  attempt_count INTEGER NOT NULL CHECK(attempt_count>=0),
  max_attempts INTEGER NOT NULL CHECK(max_attempts BETWEEN 1 AND 4),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  available_at_ms INTEGER NOT NULL CHECK(available_at_ms>=0),
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
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(kind,dedupe_key),
  CHECK((state IN ('SUCCEEDED','FAILED','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(kind NOT IN ('REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE') OR scope_type='IMPORT_ITEM'),
  CHECK(kind<>'SERVER_BIOS_IMPORT' OR scope_type='SERVER_IMPORT'),
  CHECK(kind NOT IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT') OR scope_type='PEGASUS_IMPORT'),
  CHECK((scope_type='EMULATIONSTATION_IMPORT')=(kind IN ('SERVER_EMULATIONSTATION_SCAN','SERVER_EMULATIONSTATION_IMPORT'))),
  CHECK(kind<>'REVIEW_BULK_APPROVE' OR scope_type='REVIEW_BULK_APPROVAL')
  ,CHECK(kind<>'RUNTIME_ASSET_PACK_VALIDATE' OR scope_type='RUNTIME_ASSET_PACK_INSTALLATION')
  ,CHECK(kind<>'PAYLOAD_RELEASE' OR scope_type IN (
    'IMPORT_ITEM','IMPORT_JOB','PEGASUS_IMPORT_ITEM','EMULATIONSTATION_IMPORT_ITEM','UPLOAD_CONSUMPTION','GAME'
  ))
  ,CHECK(scope_type<>'EMULATIONSTATION_IMPORT_ITEM' OR kind='PAYLOAD_RELEASE')
  ,CHECK(kind<>'PAYLOAD_RELEASE' OR (cancellable=0 AND max_attempts=4))
  ,CHECK(kind<>'BLOB_GC' OR (scope_type='BLOB' AND cancellable=0 AND max_attempts=4))
);

CREATE TABLE blob_gc_candidates (
  blob_id TEXT PRIMARY KEY REFERENCES blobs(id),
  gc_job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  first_unreferenced_at_ms INTEGER NOT NULL CHECK(first_unreferenced_at_ms >= 0),
  scheduled_at_ms INTEGER NOT NULL CHECK(scheduled_at_ms >= first_unreferenced_at_ms),
  deleted_at_ms INTEGER,
  last_failed_at_ms INTEGER,
  error_code TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0)
);

CREATE TABLE "job_events" (
  id INTEGER PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id),
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  event_type TEXT NOT NULL CHECK(event_type IN (
    'QUEUED','STARTED','PROGRESS','RETRY_SCHEDULED','CANCEL_REQUESTED','MANUAL_RETRY',
    'ARCHIVE_SCANNED','PARENT_MATCHED','PARENT_REJECTED','SOURCE_SNAPSHOT_CREATED',
    'CORE_VALIDATION_COMPLETED','PLAYLIST_PARSED','DISC_SET_MATCHED','DISC_SET_REJECTED',
    'SUCCEEDED','FAILED','CANCELLED'
  )),
  data_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)
);

CREATE TABLE job_input_snapshots (
  job_id TEXT NOT NULL REFERENCES jobs(id),
  execution_no INTEGER NOT NULL CHECK(execution_no >= 1),
  input_json TEXT NOT NULL,
  input_digest TEXT NOT NULL CHECK(length(input_digest) = 64),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  PRIMARY KEY(job_id, execution_no)
);

CREATE TABLE "idempotency_records" (
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

CREATE TABLE "audit_events" (
  id TEXT PRIMARY KEY,
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT REFERENCES users(id),
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','offline-recovery','startup-test-bootstrap','restore-security-fence',
    'payload-release-worker'
  )),
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  before_json TEXT,
  after_json TEXT,
  diff_json TEXT,
  request_id TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);
