-- retrom: rebuild-with-foreign-keys-off
-- SQLite cannot widen CHECK enumerations in place. The migration runner disables
-- foreign-key enforcement only for this transaction and verifies foreign_key_check
-- before commit; table names are restored before enforcement is re-enabled.

CREATE TABLE jobs_v19 (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE','METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP','REVIEW_ARCADE_PARENT_VALIDATE')),
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
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED')) = (cancel_requested_at_ms IS NOT NULL)),
  CHECK(kind <> 'REVIEW_ARCADE_PARENT_VALIDATE' OR scope_type = 'IMPORT_ITEM')
);

INSERT INTO jobs_v19 SELECT * FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v19 RENAME TO jobs;
CREATE INDEX jobs_claim ON jobs(state, available_at_ms);
CREATE INDEX jobs_scope ON jobs(scope_type, scope_id);

CREATE TABLE job_events_v19 (
  id INTEGER PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id),
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  event_type TEXT NOT NULL CHECK(event_type IN ('QUEUED','STARTED','PROGRESS','RETRY_SCHEDULED','CANCEL_REQUESTED','MANUAL_RETRY','ARCHIVE_SCANNED','PARENT_MATCHED','PARENT_REJECTED','SOURCE_SNAPSHOT_CREATED','CORE_VALIDATION_COMPLETED','SUCCEEDED','FAILED','CANCELLED')),
  data_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0)
);

INSERT INTO job_events_v19 SELECT * FROM job_events;
DROP TABLE job_events;
ALTER TABLE job_events_v19 RENAME TO job_events;
CREATE INDEX job_events_scope ON job_events(scope_type, scope_id, id);
CREATE INDEX job_events_job ON job_events(job_id, id);
CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TABLE upload_consumptions_v19 (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  consumer_type TEXT NOT NULL CHECK(consumer_type IN ('IMPORT_JOB','GAME_FILE_REVISION_JOB','GAME_ASSET','REVIEW_ASSET','REVIEW_ARCADE_PARENT','BIOS_INSTALLATION','DAT_VERSION')),
  consumer_id TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(consumer_type, consumer_id)
);

INSERT INTO upload_consumptions_v19 SELECT * FROM upload_consumptions;
DROP TABLE upload_consumptions;
ALTER TABLE upload_consumptions_v19 RENAME TO upload_consumptions;
CREATE UNIQUE INDEX upload_consumptions_whole_session ON upload_consumptions(upload_session_id) WHERE upload_file_id IS NULL;

CREATE TABLE import_item_source_snapshots (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  revision_no INTEGER NOT NULL CHECK(revision_no >= 1),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest) = 64 AND source_manifest_digest = lower(source_manifest_digest)),
  created_by TEXT NOT NULL CHECK(created_by IN ('IDENTIFICATION','ARCADE_PARENT_ATTACHMENT')),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  UNIQUE(import_item_id, revision_no),
  UNIQUE(import_item_id, source_manifest_digest)
);

CREATE TABLE import_item_source_snapshot_files (
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order >= 0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  PRIMARY KEY(source_snapshot_id, role, logical_name),
  FOREIGN KEY(source_archive_blob_id, source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id, ordinal),
  CHECK((source_archive_blob_id IS NULL) = (source_archive_entry_ordinal IS NULL))
);

INSERT INTO import_item_source_snapshots(
  id,import_item_id,revision_no,source_manifest_json,source_manifest_digest,created_by,created_at_ms
)
SELECT id,id,1,source_manifest_json,source_manifest_digest,'IDENTIFICATION',created_at_ms
FROM import_items;

INSERT INTO import_item_source_snapshot_files(
  source_snapshot_id,role,logical_name,upload_file_id,blob_id,
  source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms
)
SELECT import_item_id,role,logical_name,upload_file_id,blob_id,
source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms
FROM import_item_source_files;

ALTER TABLE import_item_core_validations
ADD COLUMN source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id);

DROP TRIGGER import_item_core_validations_immutable_update;
UPDATE import_item_core_validations
SET source_snapshot_id=(
  SELECT snapshot.id
  FROM import_item_source_snapshots snapshot
  WHERE snapshot.import_item_id=import_item_core_validations.import_item_id
  AND snapshot.source_manifest_digest=import_item_core_validations.source_manifest_digest
);

CREATE TABLE migration_019_validation_assert(value INTEGER NOT NULL CHECK(value=0));
INSERT INTO migration_019_validation_assert
SELECT count(*) FROM import_item_core_validations WHERE source_snapshot_id IS NULL;
DROP TABLE migration_019_validation_assert;
CREATE TRIGGER import_item_core_validations_immutable_update BEFORE UPDATE ON import_item_core_validations BEGIN SELECT RAISE(ABORT, 'immutable'); END;

ALTER TABLE review_drafts
ADD COLUMN effective_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id);

UPDATE review_drafts
SET effective_source_snapshot_id=(
  SELECT snapshot.id
  FROM import_item_source_snapshots snapshot
  WHERE snapshot.import_item_id=review_drafts.import_item_id
  AND snapshot.revision_no=1
);

CREATE TABLE migration_019_draft_assert(value INTEGER NOT NULL CHECK(value=0));
INSERT INTO migration_019_draft_assert
SELECT count(*) FROM review_drafts WHERE effective_source_snapshot_id IS NULL;
INSERT INTO migration_019_draft_assert
SELECT count(*)
FROM review_drafts draft
JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE validation.status<>'READY'
OR validation.import_item_id<>draft.import_item_id
OR validation.source_snapshot_id<>draft.effective_source_snapshot_id;
DROP TABLE migration_019_draft_assert;

CREATE TRIGGER import_item_source_snapshots_revision_insert
BEFORE INSERT ON import_item_source_snapshots
WHEN NEW.revision_no<>(SELECT COALESCE(MAX(revision_no),0)+1 FROM import_item_source_snapshots WHERE import_item_id=NEW.import_item_id)
BEGIN SELECT RAISE(ABORT, 'source snapshot revision must be contiguous'); END;
CREATE TRIGGER import_item_source_snapshots_immutable_update BEFORE UPDATE ON import_item_source_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_source_snapshots_immutable_delete BEFORE DELETE ON import_item_source_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_source_snapshot_files_immutable_update BEFORE UPDATE ON import_item_source_snapshot_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_source_snapshot_files_immutable_delete BEFORE DELETE ON import_item_source_snapshot_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER import_item_core_validation_snapshot_insert
BEFORE INSERT ON import_item_core_validations
WHEN NEW.source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.source_snapshot_id
  AND snapshot.import_item_id=NEW.import_item_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
)
BEGIN SELECT RAISE(ABORT, 'invalid validation source snapshot'); END;

CREATE TRIGGER review_drafts_source_snapshot_insert
BEFORE INSERT ON review_drafts
WHEN NEW.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.effective_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT, 'invalid review source snapshot'); END;
CREATE TRIGGER review_drafts_source_snapshot_update
BEFORE UPDATE OF import_item_id,effective_source_snapshot_id ON review_drafts
WHEN NEW.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.effective_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT, 'invalid review source snapshot'); END;
CREATE TRIGGER review_drafts_final_source_snapshot_update
BEFORE UPDATE OF effective_source_snapshot_id ON review_drafts
WHEN NEW.effective_source_snapshot_id<>OLD.effective_source_snapshot_id
AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=OLD.import_item_id AND item.state<>'REVIEW_PENDING')
BEGIN SELECT RAISE(ABORT, 'finalized review source snapshot'); END;
CREATE TRIGGER review_drafts_validation_snapshot_insert
BEFORE INSERT ON review_drafts
WHEN NEW.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.selected_validation_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.effective_source_snapshot_id
  AND validation.status='READY'
)
BEGIN SELECT RAISE(ABORT, 'invalid selected validation snapshot'); END;
CREATE TRIGGER review_drafts_validation_snapshot_update
BEFORE UPDATE OF import_item_id,effective_source_snapshot_id,selected_validation_id ON review_drafts
WHEN NEW.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.selected_validation_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.effective_source_snapshot_id
  AND validation.status='READY'
)
BEGIN SELECT RAISE(ABORT, 'invalid selected validation snapshot'); END;

CREATE TRIGGER game_content_revisions_review_snapshot_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='IMPORT_REVIEW'
AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=NEW.source_ref_id)
AND NOT EXISTS(
  SELECT 1
  FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
  WHERE draft.import_item_id=NEW.source_ref_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
)
BEGIN SELECT RAISE(ABORT, 'review content source snapshot mismatch'); END;

CREATE TABLE review_arcade_parent_attachments (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  base_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  result_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  dependency_machine TEXT NOT NULL CHECK(length(CAST(dependency_machine AS BLOB)) BETWEEN 1 AND 255),
  expected_logical_name TEXT NOT NULL,
  required_by_machine TEXT NOT NULL CHECK(length(CAST(required_by_machine AS BLOB)) BETWEEN 1 AND 255),
  depth INTEGER NOT NULL CHECK(depth BETWEEN 1 AND 63),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  dat_version_id TEXT NOT NULL REFERENCES dat_versions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  accepted_blob_id TEXT REFERENCES blobs(id),
  original_filename TEXT NOT NULL CHECK(length(CAST(original_filename AS BLOB)) BETWEEN 1 AND 255),
  observed_size_bytes INTEGER CHECK(observed_size_bytes IS NULL OR observed_size_bytes >= 0),
  observed_sha256 TEXT CHECK(observed_sha256 IS NULL OR (length(observed_sha256)=64 AND observed_sha256=lower(observed_sha256))),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(length(CAST(diagnostics_json AS BLOB)) <= 65536),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  finished_at_ms INTEGER,
  CHECK(expected_logical_name=dependency_machine||'.zip'),
  CHECK((state='ACCEPTED')=(accepted_blob_id IS NOT NULL AND result_source_snapshot_id IS NOT NULL)),
  CHECK((state IN ('REJECTED','FAILED_RETRYABLE','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK(state IN ('QUEUED','RUNNING') OR upload_file_id IS NULL OR observed_size_bytes IS NOT NULL)
);

CREATE UNIQUE INDEX review_arcade_parent_active
ON review_arcade_parent_attachments(import_item_id)
WHERE state IN ('QUEUED','RUNNING');
CREATE INDEX review_arcade_parent_history
ON review_arcade_parent_attachments(import_item_id,created_at_ms,id);

CREATE TRIGGER review_arcade_parent_owner_insert
BEFORE INSERT ON review_arcade_parent_attachments
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.base_source_snapshot_id
  WHERE draft.id=NEW.review_draft_id
  AND draft.import_item_id=NEW.import_item_id
  AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT, 'invalid attachment owner'); END;
CREATE TRIGGER review_arcade_parent_transition_update
BEFORE UPDATE OF state ON review_arcade_parent_attachments
WHEN NOT (
  OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELLED','FAILED_RETRYABLE') OR
  OLD.state='RUNNING' AND NEW.state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED') OR
  OLD.state='FAILED_RETRYABLE' AND NEW.state IN ('QUEUED','RUNNING','CANCELLED')
)
BEGIN SELECT RAISE(ABORT, 'invalid attachment state transition'); END;

CREATE TABLE review_events_v19 (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  event_type TEXT NOT NULL CHECK(event_type IN ('DRAFT_SAVED','TARGET_CHANGED','SCRAPE_REQUESTED','CANDIDATE_APPLIED','CANDIDATE_REMOVED','PARENT_UPLOAD_REQUESTED','PARENT_ATTACHMENT_ACCEPTED','PARENT_ATTACHMENT_REJECTED','APPROVED','DISCARDED')),
  actor TEXT NOT NULL CHECK(actor = 'local'),
  before_json TEXT NOT NULL,
  after_json TEXT NOT NULL,
  diff_json TEXT NOT NULL,
  config_evidence_json TEXT NOT NULL,
  dat_evidence_json TEXT NOT NULL,
  provider_evidence_json TEXT NOT NULL,
  reason TEXT,
  created_at_ms INTEGER NOT NULL
);

INSERT INTO review_events_v19 SELECT * FROM review_events;
DROP TABLE review_events;
ALTER TABLE review_events_v19 RENAME TO review_events;
CREATE INDEX review_events_history ON review_events(event_type, created_at_ms, id);
CREATE TRIGGER review_events_immutable_update BEFORE UPDATE ON review_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER review_events_immutable_delete BEFORE DELETE ON review_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
