-- retrom: rebuild-with-foreign-keys-off
-- Resumable strict-READY bulk review approval.

DROP TRIGGER review_multidisc_attachment_owner_insert;
DROP TRIGGER server_import_job_insert;
DROP TRIGGER pegasus_import_scan_job_insert;
DROP TRIGGER pegasus_import_job_update;

CREATE TABLE jobs_v37 (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE','SERVER_BIOS_IMPORT',
    'SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT','REVIEW_BULK_APPROVE'
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
  CHECK(kind<>'REVIEW_BULK_APPROVE' OR scope_type='REVIEW_BULK_APPROVAL')
);
INSERT INTO jobs_v37 SELECT * FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v37 RENAME TO jobs;
CREATE INDEX jobs_claim ON jobs(state,available_at_ms);
CREATE INDEX jobs_scope ON jobs(scope_type,scope_id);

CREATE TABLE job_events_v37 (
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
INSERT INTO job_events_v37 SELECT * FROM job_events;
DROP TABLE job_events;
ALTER TABLE job_events_v37 RENAME TO job_events;
CREATE INDEX job_events_scope ON job_events(scope_type,scope_id,id);
CREATE INDEX job_events_job ON job_events(job_id,id);
CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_multidisc_attachment_owner_insert
BEFORE INSERT ON review_multidisc_attachments
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.base_source_snapshot_id
  JOIN jobs job ON job.id=NEW.job_id
  WHERE draft.id=NEW.review_draft_id AND draft.import_item_id=NEW.import_item_id
  AND draft.effective_source_snapshot_id=NEW.base_source_snapshot_id
  AND snapshot.import_item_id=NEW.import_item_id AND snapshot.content_kind='MULTI_DISC_M3U_V1'
  AND job.scope_type='IMPORT_ITEM' AND job.scope_id=NEW.import_item_id
  AND job.kind='REVIEW_MULTI_DISC_VALIDATE'
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment owner'); END;

CREATE TRIGGER server_import_job_insert
BEFORE INSERT ON server_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.job_id AND job.kind='SERVER_BIOS_IMPORT'
  AND job.scope_type='SERVER_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid server import job'); END;

CREATE TRIGGER pegasus_import_scan_job_insert
BEFORE INSERT ON pegasus_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.scan_job_id AND job.kind='SERVER_PEGASUS_SCAN'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus scan job'); END;

CREATE TRIGGER pegasus_import_job_update
BEFORE UPDATE OF import_job_id ON pegasus_imports
WHEN NEW.import_job_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.import_job_id AND job.kind='SERVER_PEGASUS_IMPORT'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus import job'); END;

CREATE TABLE review_bulk_approvals (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  state TEXT NOT NULL CHECK(state IN (
    'QUEUED','RUNNING','CANCEL_REQUESTED','COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED'
  )),
  scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),
  scope_digest TEXT NOT NULL CHECK(length(scope_digest)=64 AND scope_digest=lower(scope_digest)),
  candidate_manifest_digest TEXT NOT NULL CHECK(
    length(candidate_manifest_digest)=64 AND candidate_manifest_digest=lower(candidate_manifest_digest)
  ),
  matched_count INTEGER NOT NULL CHECK(matched_count>=0),
  candidate_count INTEGER NOT NULL CHECK(candidate_count BETWEEN 1 AND 10000),
  screenshot_only_count INTEGER NOT NULL CHECK(screenshot_only_count>=0),
  duplicate_count INTEGER NOT NULL CHECK(duplicate_count>=0),
  attachment_active_count INTEGER NOT NULL CHECK(attachment_active_count>=0),
  not_ready_or_stale_count INTEGER NOT NULL CHECK(not_ready_or_stale_count>=0),
  processed_count INTEGER NOT NULL DEFAULT 0 CHECK(processed_count>=0 AND processed_count<=candidate_count),
  published_count INTEGER NOT NULL DEFAULT 0 CHECK(published_count>=0),
  skipped_duplicate_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_duplicate_count>=0),
  skipped_changed_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_changed_count>=0),
  skipped_not_ready_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_not_ready_count>=0),
  failed_count INTEGER NOT NULL DEFAULT 0 CHECK(failed_count>=0),
  cancelled_count INTEGER NOT NULL DEFAULT 0 CHECK(cancelled_count>=0),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  last_error_code TEXT,
  cancel_reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  started_at_ms INTEGER,
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  cancel_requested_at_ms INTEGER,
  CHECK((state IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED'))=(completed_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(state NOT IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED') OR processed_count=candidate_count),
  CHECK(processed_count=published_count+skipped_duplicate_count+skipped_changed_count+
    skipped_not_ready_count+failed_count+cancelled_count)
);
CREATE UNIQUE INDEX review_bulk_approvals_one_active ON review_bulk_approvals((1))
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');
CREATE INDEX review_bulk_approvals_history ON review_bulk_approvals(created_at_ms DESC,id DESC);

CREATE TABLE review_bulk_approval_items (
  bulk_approval_id TEXT NOT NULL REFERENCES review_bulk_approvals(id),
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  ordinal INTEGER NOT NULL CHECK(ordinal>=0),
  expected_review_version INTEGER NOT NULL CHECK(expected_review_version>=1),
  expected_validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  expected_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  title_snapshot TEXT NOT NULL CHECK(length(title_snapshot) BETWEEN 1 AND 200),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  target_platform_name_snapshot TEXT NOT NULL CHECK(length(target_platform_name_snapshot) BETWEEN 1 AND 120),
  state TEXT NOT NULL CHECK(state IN (
    'PENDING','RUNNING','PUBLISHED','SKIPPED_DUPLICATE','SKIPPED_CHANGED','SKIPPED_NOT_READY',
    'FAILED_FINAL','CANCELLED'
  )),
  game_id TEXT REFERENCES games(id),
  review_event_id TEXT REFERENCES review_events(id),
  outcome_code TEXT,
  outcome_details_json TEXT CHECK(
    outcome_details_json IS NULL OR (json_valid(outcome_details_json) AND length(CAST(outcome_details_json AS BLOB))<=8192)
  ),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  started_at_ms INTEGER,
  completed_at_ms INTEGER,
  PRIMARY KEY(bulk_approval_id,import_item_id),
  UNIQUE(bulk_approval_id,ordinal),
  CHECK((state IN ('PENDING','RUNNING'))=(completed_at_ms IS NULL)),
  CHECK((state='PUBLISHED')=(game_id IS NOT NULL AND review_event_id IS NOT NULL))
);
CREATE INDEX review_bulk_approval_items_state ON review_bulk_approval_items(bulk_approval_id,state,ordinal);

CREATE TRIGGER review_bulk_approval_job_insert
BEFORE INSERT ON review_bulk_approvals
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.job_id AND job.kind='REVIEW_BULK_APPROVE'
  AND job.scope_type='REVIEW_BULK_APPROVAL' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid review bulk approval job'); END;

CREATE TRIGGER review_bulk_approvals_frozen_update
BEFORE UPDATE OF job_id,scope_json,scope_digest,candidate_manifest_digest,matched_count,candidate_count,
screenshot_only_count,duplicate_count,attachment_active_count,not_ready_or_stale_count,
created_by_user_id,created_at_ms ON review_bulk_approvals
BEGIN SELECT RAISE(ABORT,'immutable review bulk approval input'); END;

CREATE TRIGGER review_bulk_approval_items_owner_insert
BEFORE INSERT ON review_bulk_approval_items
WHEN NOT EXISTS(
  SELECT 1 FROM import_items item
  JOIN review_drafts draft ON draft.import_item_id=item.id
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.expected_source_snapshot_id
  JOIN import_item_core_validations validation ON validation.id=NEW.expected_validation_id
  WHERE item.id=NEW.import_item_id AND item.state='REVIEW_PENDING'
  AND draft.version=NEW.expected_review_version
  AND draft.effective_source_snapshot_id=NEW.expected_source_snapshot_id
  AND draft.target_platform_instance_id=NEW.target_platform_instance_id
  AND snapshot.import_item_id=NEW.import_item_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.expected_source_snapshot_id
  AND validation.target_platform_instance_id=NEW.target_platform_instance_id
)
BEGIN SELECT RAISE(ABORT,'invalid review bulk approval item input'); END;

CREATE TRIGGER review_bulk_approval_items_frozen_update
BEFORE UPDATE OF bulk_approval_id,import_item_id,ordinal,expected_review_version,expected_validation_id,
expected_source_snapshot_id,title_snapshot,target_platform_instance_id,target_platform_name_snapshot,
created_at_ms ON review_bulk_approval_items
BEGIN SELECT RAISE(ABORT,'immutable review bulk approval item input'); END;

CREATE TRIGGER review_bulk_approval_items_published_update
BEFORE UPDATE OF state,game_id,review_event_id ON review_bulk_approval_items
WHEN NEW.state='PUBLISHED' AND NOT EXISTS(
  SELECT 1 FROM review_events event
  JOIN games game ON game.id=NEW.game_id
  WHERE event.id=NEW.review_event_id AND event.import_item_id=NEW.import_item_id
  AND event.event_type='APPROVED' AND json_extract(event.after_json,'$.gameId')=NEW.game_id
)
BEGIN SELECT RAISE(ABORT,'invalid review bulk approval published result'); END;
