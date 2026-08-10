-- retrom: rebuild-with-foreign-keys-off
-- Multi-disc support widens several CHECK enumerations. The migration runner
-- disables foreign keys for this transaction and runs foreign_key_check before
-- commit. Only schema 023 databases may reach this migration.

CREATE TABLE migration_024_layout_assert(value INTEGER NOT NULL CHECK(value=0));
INSERT INTO migration_024_layout_assert
SELECT count(*)
FROM import_item_source_snapshots snapshot
WHERE EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=snapshot.id AND file.role='DOS_SOURCE'
)
AND EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=snapshot.id AND file.role IN ('CONTENT','COMPANION')
);
INSERT INTO migration_024_layout_assert
SELECT count(*)
FROM game_content_revisions revision
WHERE EXISTS(
  SELECT 1 FROM game_content_files file
  WHERE file.game_content_revision_id=revision.id AND file.role='DOS_SOURCE'
)
AND EXISTS(
  SELECT 1 FROM game_content_files file
  WHERE file.game_content_revision_id=revision.id AND file.role IN ('CONTENT','COMPANION')
);
INSERT INTO migration_024_layout_assert
SELECT count(*)
FROM import_item_core_validations validation
LEFT JOIN core_artifacts artifact ON artifact.id=validation.core_artifact_id
WHERE artifact.id IS NULL OR artifact.version<1;
DROP TABLE migration_024_layout_assert;

-- Drop cross-table triggers before their referenced tables are rebuilt. They
-- are recreated after all final table names and columns are in place.
DROP TRIGGER import_item_core_validation_snapshot_insert;
DROP TRIGGER review_drafts_source_snapshot_insert;
DROP TRIGGER review_drafts_source_snapshot_update;
DROP TRIGGER review_drafts_validation_snapshot_insert;
DROP TRIGGER review_drafts_validation_snapshot_update;
DROP TRIGGER game_content_revisions_review_snapshot_insert;
DROP TRIGGER review_arcade_parent_owner_insert;
DROP TRIGGER games_current_metadata_owner_insert;
DROP TRIGGER games_current_owner_update;

CREATE TABLE jobs_v24 (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE'
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
  CHECK(kind NOT IN ('REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE') OR scope_type='IMPORT_ITEM')
);
INSERT INTO jobs_v24 SELECT * FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v24 RENAME TO jobs;
CREATE INDEX jobs_claim ON jobs(state,available_at_ms);
CREATE INDEX jobs_scope ON jobs(scope_type,scope_id);

CREATE TABLE job_events_v24 (
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
INSERT INTO job_events_v24 SELECT * FROM job_events;
DROP TABLE job_events;
ALTER TABLE job_events_v24 RENAME TO job_events;
CREATE INDEX job_events_scope ON job_events(scope_type,scope_id,id);
CREATE INDEX job_events_job ON job_events(job_id,id);
CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE upload_consumptions_v24 (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  consumer_type TEXT NOT NULL CHECK(consumer_type IN (
    'IMPORT_JOB','GAME_FILE_REVISION_JOB','GAME_ASSET','REVIEW_ASSET','REVIEW_ARCADE_PARENT',
    'REVIEW_MULTI_DISC','BIOS_INSTALLATION','DAT_VERSION'
  )),
  consumer_id TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(consumer_type,consumer_id)
);
INSERT INTO upload_consumptions_v24 SELECT * FROM upload_consumptions;
DROP TABLE upload_consumptions;
ALTER TABLE upload_consumptions_v24 RENAME TO upload_consumptions;
CREATE UNIQUE INDEX upload_consumptions_whole_session
ON upload_consumptions(upload_session_id) WHERE upload_file_id IS NULL;

CREATE TABLE import_item_source_files_v24 (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_item_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);
INSERT INTO import_item_source_files_v24 SELECT * FROM import_item_source_files;
DROP TABLE import_item_source_files;
ALTER TABLE import_item_source_files_v24 RENAME TO import_item_source_files;
CREATE TRIGGER import_item_source_files_immutable_update
BEFORE UPDATE ON import_item_source_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_source_files_immutable_delete
BEFORE DELETE ON import_item_source_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE import_item_source_snapshots_v24 (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  revision_no INTEGER NOT NULL CHECK(revision_no>=1),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE'
    CHECK(content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL
    CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  created_by TEXT NOT NULL CHECK(created_by IN ('IDENTIFICATION','ARCADE_PARENT_ATTACHMENT','MULTI_DISC_ATTACHMENT')),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(import_item_id,revision_no),
  UNIQUE(import_item_id,source_manifest_digest)
);
INSERT INTO import_item_source_snapshots_v24(
  id,import_item_id,revision_no,content_kind,source_manifest_json,source_manifest_digest,created_by,created_at_ms
)
SELECT snapshot.id,snapshot.import_item_id,snapshot.revision_no,
       CASE WHEN EXISTS(
         SELECT 1 FROM import_item_source_snapshot_files file
         WHERE file.source_snapshot_id=snapshot.id AND file.role='DOS_SOURCE'
       ) THEN 'DOS_BUNDLE' ELSE 'SINGLE_FILE' END,
       snapshot.source_manifest_json,snapshot.source_manifest_digest,snapshot.created_by,snapshot.created_at_ms
FROM import_item_source_snapshots snapshot;
DROP TABLE import_item_source_snapshots;
ALTER TABLE import_item_source_snapshots_v24 RENAME TO import_item_source_snapshots;
CREATE TRIGGER import_item_source_snapshots_revision_insert
BEFORE INSERT ON import_item_source_snapshots
WHEN NEW.revision_no<>(
  SELECT COALESCE(MAX(revision_no),0)+1 FROM import_item_source_snapshots WHERE import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'source snapshot revision must be contiguous'); END;
CREATE TRIGGER import_item_source_snapshots_immutable_update
BEFORE UPDATE ON import_item_source_snapshots BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_source_snapshots_immutable_delete
BEFORE DELETE ON import_item_source_snapshots BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE import_item_source_snapshot_files_v24 (
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(source_snapshot_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);
INSERT INTO import_item_source_snapshot_files_v24 SELECT * FROM import_item_source_snapshot_files;
DROP TABLE import_item_source_snapshot_files;
ALTER TABLE import_item_source_snapshot_files_v24 RENAME TO import_item_source_snapshot_files;
CREATE TRIGGER import_item_source_snapshot_files_immutable_update
BEFORE UPDATE ON import_item_source_snapshot_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_source_snapshot_files_immutable_delete
BEFORE DELETE ON import_item_source_snapshot_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE import_item_core_validations_v24 (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  platform_instance_version INTEGER NOT NULL CHECK(platform_instance_version>=1),
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  core_artifact_version INTEGER NOT NULL DEFAULT 1 CHECK(core_artifact_version>=1),
  prepublish_generation INTEGER NOT NULL DEFAULT 4 CHECK(prepublish_generation IN (3,4)),
  dat_version_id TEXT REFERENCES dat_versions(id),
  default_dos_entry TEXT,
  source_manifest_digest TEXT NOT NULL,
  prepublish_input_digest TEXT NOT NULL CHECK(length(prepublish_input_digest)=64),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  UNIQUE(import_item_id,prepublish_input_digest)
);
INSERT INTO import_item_core_validations_v24(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,core_artifact_id,
  core_artifact_version,prepublish_generation,dat_version_id,default_dos_entry,source_manifest_digest,
  prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms,source_snapshot_id
)
SELECT validation.id,validation.import_item_id,validation.target_platform_instance_id,
       validation.platform_instance_version,validation.core_id,validation.core_artifact_id,
       artifact.version,3,validation.dat_version_id,validation.default_dos_entry,validation.source_manifest_digest,
       validation.prepublish_input_digest,validation.status,validation.compatibility_code,
       validation.dependency_snapshot_json,validation.created_at_ms,validation.source_snapshot_id
FROM import_item_core_validations validation
JOIN core_artifacts artifact ON artifact.id=validation.core_artifact_id;
DROP TABLE import_item_core_validations;
ALTER TABLE import_item_core_validations_v24 RENAME TO import_item_core_validations;
CREATE TRIGGER import_item_core_validations_immutable_update
BEFORE UPDATE ON import_item_core_validations BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_core_validations_immutable_delete
BEFORE DELETE ON import_item_core_validations BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_core_validation_snapshot_insert
BEFORE INSERT ON import_item_core_validations
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.source_snapshot_id
  AND snapshot.import_item_id=NEW.import_item_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
)
BEGIN SELECT RAISE(ABORT,'invalid validation source snapshot'); END;
CREATE TRIGGER import_item_core_validation_artifact_insert
BEFORE INSERT ON import_item_core_validations
WHEN NEW.prepublish_generation<>4 OR NOT EXISTS(
  SELECT 1 FROM core_artifacts artifact
  WHERE artifact.id=NEW.core_artifact_id AND artifact.core_id=NEW.core_id
  AND artifact.version=NEW.core_artifact_version
)
BEGIN SELECT RAISE(ABORT,'invalid validation generation or artifact version'); END;

CREATE TRIGGER review_drafts_source_snapshot_insert
BEFORE INSERT ON review_drafts
WHEN NEW.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.effective_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'invalid review source snapshot'); END;
CREATE TRIGGER review_drafts_source_snapshot_update
BEFORE UPDATE OF import_item_id,effective_source_snapshot_id ON review_drafts
WHEN NEW.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.effective_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'invalid review source snapshot'); END;
CREATE TRIGGER review_arcade_parent_owner_insert
BEFORE INSERT ON review_arcade_parent_attachments
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.base_source_snapshot_id
  WHERE draft.id=NEW.review_draft_id
  AND draft.import_item_id=NEW.import_item_id
  AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'invalid attachment owner'); END;

CREATE TRIGGER review_drafts_validation_snapshot_insert
BEFORE INSERT ON review_drafts
WHEN NEW.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.selected_validation_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.effective_source_snapshot_id
  AND validation.status='READY' AND validation.prepublish_generation=4
)
BEGIN SELECT RAISE(ABORT,'invalid selected validation snapshot'); END;
CREATE TRIGGER review_drafts_validation_snapshot_update
BEFORE UPDATE OF import_item_id,effective_source_snapshot_id,selected_validation_id ON review_drafts
WHEN NEW.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.selected_validation_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.effective_source_snapshot_id
  AND validation.status='READY' AND validation.prepublish_generation=4
)
BEGIN SELECT RAISE(ABORT,'invalid selected validation snapshot'); END;

CREATE TABLE import_item_validation_files_v24 (
  import_item_core_validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_item_core_validation_id,role,logical_name)
);
INSERT INTO import_item_validation_files_v24 SELECT * FROM import_item_validation_files;
DROP TABLE import_item_validation_files;
ALTER TABLE import_item_validation_files_v24 RENAME TO import_item_validation_files;
CREATE TRIGGER import_item_validation_files_immutable_update
BEFORE UPDATE ON import_item_validation_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_validation_files_immutable_delete
BEFORE DELETE ON import_item_validation_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE game_content_revisions_v24 (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE'
    CHECK(content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_REPLACE')),
  source_ref_id TEXT NOT NULL,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(id,game_id)
);
INSERT INTO game_content_revisions_v24(
  id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
)
SELECT revision.id,revision.game_id,
       CASE WHEN EXISTS(
         SELECT 1 FROM game_content_files file
         WHERE file.game_content_revision_id=revision.id AND file.role='DOS_SOURCE'
       ) THEN 'DOS_BUNDLE' ELSE 'SINGLE_FILE' END,
       revision.source_kind,revision.source_ref_id,revision.source_manifest_json,
       revision.source_manifest_digest,revision.created_at_ms
FROM game_content_revisions revision;
DROP TABLE game_content_revisions;
ALTER TABLE game_content_revisions_v24 RENAME TO game_content_revisions;
CREATE INDEX fk_game_content_game ON game_content_revisions(game_id);
CREATE TRIGGER game_content_revisions_immutable_update
BEFORE UPDATE ON game_content_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_content_revisions_immutable_delete
BEFORE DELETE ON game_content_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_content_revisions_review_snapshot_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='IMPORT_REVIEW' AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=NEW.source_ref_id)
AND NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
  WHERE draft.import_item_id=NEW.source_ref_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
  AND snapshot.content_kind=NEW.content_kind
)
BEGIN SELECT RAISE(ABORT,'review content source snapshot mismatch'); END;
CREATE TRIGGER games_current_metadata_owner_insert
BEFORE INSERT ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_metadata_revisions
    WHERE id=NEW.current_metadata_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_content_revisions
    WHERE id=NEW.current_content_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current content owner mismatch') END;
END;
CREATE TRIGGER games_current_owner_update
BEFORE UPDATE OF current_metadata_revision_id,current_content_revision_id ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_metadata_revisions
    WHERE id=NEW.current_metadata_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_content_revisions
    WHERE id=NEW.current_content_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current content owner mismatch') END;
END;

CREATE TABLE game_content_files_v24 (
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_content_revision_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);
INSERT INTO game_content_files_v24 SELECT * FROM game_content_files;
DROP TABLE game_content_files;
ALTER TABLE game_content_files_v24 RENAME TO game_content_files;
CREATE TRIGGER game_content_files_immutable_update
BEFORE UPDATE ON game_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_content_files_immutable_delete
BEFORE DELETE ON game_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE variant_files_v24 (
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_variant_revision_id,role,logical_name)
);
INSERT INTO variant_files_v24 SELECT * FROM variant_files;
DROP TABLE variant_files;
ALTER TABLE variant_files_v24 RENAME TO variant_files;
CREATE TRIGGER variant_files_immutable_update
BEFORE UPDATE ON variant_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER variant_files_immutable_delete
BEFORE DELETE ON variant_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE launch_content_files_v24 (
  launch_session_id TEXT PRIMARY KEY REFERENCES launch_sessions(id),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 512),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  format_version TEXT NOT NULL CHECK(format_version IN (
    'SOURCE_V1','RETROM_DOS_DIRECT_ZIP_V1','RETROM_MULTIDISC_M3U_V1'
  )),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)
);
INSERT INTO launch_content_files_v24 SELECT * FROM launch_content_files;
DROP TABLE launch_content_files;
ALTER TABLE launch_content_files_v24 RENAME TO launch_content_files;
CREATE TRIGGER launch_content_files_immutable_update
BEFORE UPDATE ON launch_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER launch_content_files_immutable_delete
BEFORE DELETE ON launch_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE review_events_v24 (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  event_type TEXT NOT NULL CHECK(event_type IN (
    'DRAFT_SAVED','TARGET_CHANGED','SCRAPE_REQUESTED','CANDIDATE_APPLIED','CANDIDATE_REMOVED',
    'PARENT_UPLOAD_REQUESTED','PARENT_ATTACHMENT_ACCEPTED','PARENT_ATTACHMENT_REJECTED',
    'DISC_UPLOAD_REQUESTED','DISC_ATTACHMENT_ACCEPTED','DISC_ATTACHMENT_REJECTED','APPROVED','DISCARDED'
  )),
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT REFERENCES users(id),
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','offline-recovery','startup-test-bootstrap','restore-security-fence'
  )),
  before_json TEXT NOT NULL,
  after_json TEXT NOT NULL,
  diff_json TEXT NOT NULL,
  config_evidence_json TEXT NOT NULL,
  dat_evidence_json TEXT NOT NULL,
  provider_evidence_json TEXT NOT NULL,
  reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);
INSERT INTO review_events_v24 SELECT * FROM review_events;
DROP TABLE review_events;
ALTER TABLE review_events_v24 RENAME TO review_events;
CREATE INDEX review_events_history ON review_events(event_type,created_at_ms,id);
CREATE INDEX review_events_actor ON review_events(actor_user_id,created_at_ms,id);
CREATE TRIGGER review_events_immutable_update
BEFORE UPDATE ON review_events BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER review_events_immutable_delete
BEFORE DELETE ON review_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

ALTER TABLE launch_external_files
ADD COLUMN kind TEXT NOT NULL DEFAULT 'BIOS' CHECK(kind IN ('BIOS','DISC'));
ALTER TABLE launch_sessions
ADD COLUMN initial_disc_index INTEGER NOT NULL DEFAULT 0 CHECK(initial_disc_index BETWEEN 0 AND 7);
ALTER TABLE save_states
ADD COLUMN disc_index INTEGER CHECK(disc_index BETWEEN 0 AND 7);

CREATE TRIGGER launch_external_files_kind_insert
BEFORE INSERT ON launch_external_files
WHEN NEW.kind='DISC' AND NOT EXISTS(
  SELECT 1 FROM launch_content_files content
  WHERE content.launch_session_id=NEW.launch_session_id
  AND content.format_version='RETROM_MULTIDISC_M3U_V1'
)
BEGIN SELECT RAISE(ABORT,'disc external file requires multi-disc launch content'); END;

CREATE TABLE import_item_multidisc_entries (
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 7),
  source_reference TEXT NOT NULL,
  normalized_reference TEXT NOT NULL,
  canonical_name TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('PRESENT','MISSING')),
  upload_file_id TEXT REFERENCES upload_files(id),
  blob_id TEXT REFERENCES blobs(id),
  source_logical_name TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(source_snapshot_id,ordinal),
  UNIQUE(source_snapshot_id,normalized_reference),
  UNIQUE(source_snapshot_id,canonical_name),
  CHECK(length(CAST(source_reference AS BLOB)) BETWEEN 1 AND 255),
  CHECK(length(CAST(normalized_reference AS BLOB)) BETWEEN 1 AND 255),
  CHECK(canonical_name=printf('disc-%03d.chd',ordinal+1)),
  CHECK(
    state='PRESENT' AND upload_file_id IS NOT NULL AND blob_id IS NOT NULL AND source_logical_name IS NOT NULL OR
    state='MISSING' AND upload_file_id IS NULL AND blob_id IS NULL AND source_logical_name IS NULL
  )
);
CREATE INDEX fk_import_item_multidisc_upload ON import_item_multidisc_entries(upload_file_id);
CREATE INDEX fk_import_item_multidisc_blob ON import_item_multidisc_entries(blob_id);
CREATE TRIGGER import_item_multidisc_entries_owner_insert
BEFORE INSERT ON import_item_multidisc_entries
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.source_snapshot_id AND snapshot.content_kind='MULTI_DISC_M3U_V1'
)
OR NEW.state='PRESENT' AND NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='DISC'
  AND file.upload_file_id=NEW.upload_file_id AND file.blob_id=NEW.blob_id
  AND file.logical_name=NEW.source_logical_name AND file.sort_order=NEW.ordinal
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc entry owner'); END;
CREATE TRIGGER import_item_multidisc_entries_immutable_update
BEFORE UPDATE ON import_item_multidisc_entries BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER import_item_multidisc_entries_immutable_delete
BEFORE DELETE ON import_item_multidisc_entries BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE review_multidisc_attachments (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  requested_by_user_id TEXT NOT NULL REFERENCES users(id),
  base_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  result_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  result_validation_id TEXT REFERENCES import_item_core_validations(id),
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  expected_set_digest TEXT NOT NULL
    CHECK(length(expected_set_digest)=64 AND expected_set_digest=lower(expected_set_digest)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(length(CAST(diagnostics_json AS BLOB))<=65536),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  finished_at_ms INTEGER,
  CHECK(
    state='ACCEPTED' AND result_source_snapshot_id IS NOT NULL AND result_validation_id IS NOT NULL OR
    state<>'ACCEPTED' AND result_source_snapshot_id IS NULL AND result_validation_id IS NULL
  ),
  CHECK((state IN ('REJECTED','FAILED_RETRYABLE','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED'))=(finished_at_ms IS NOT NULL))
);
CREATE UNIQUE INDEX review_multidisc_attachment_active
ON review_multidisc_attachments(import_item_id) WHERE state IN ('QUEUED','RUNNING');
CREATE INDEX review_multidisc_attachment_history
ON review_multidisc_attachments(import_item_id,created_at_ms,id);
CREATE INDEX review_multidisc_attachment_actor
ON review_multidisc_attachments(requested_by_user_id,created_at_ms,id);
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
CREATE TRIGGER review_multidisc_attachment_identity_update
BEFORE UPDATE OF import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,
upload_session_id,expected_set_digest,job_id,created_at_ms ON review_multidisc_attachments
BEGIN SELECT RAISE(ABORT,'multi-disc attachment identity is immutable'); END;
CREATE TRIGGER review_multidisc_attachment_transition_update
BEFORE UPDATE OF state ON review_multidisc_attachments
WHEN NOT (
  OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELLED') OR
  OLD.state='RUNNING' AND NEW.state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED') OR
  OLD.state='FAILED_RETRYABLE' AND NEW.state IN ('RUNNING','CANCELLED')
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment state transition'); END;
CREATE TRIGGER review_multidisc_attachment_result_update
BEFORE UPDATE OF state,result_source_snapshot_id,result_validation_id ON review_multidisc_attachments
WHEN NEW.state='ACCEPTED' AND NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  JOIN import_item_core_validations validation ON validation.id=NEW.result_validation_id
  WHERE snapshot.id=NEW.result_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
  AND snapshot.content_kind='MULTI_DISC_M3U_V1' AND snapshot.created_by='MULTI_DISC_ATTACHMENT'
  AND snapshot.revision_no=(
    SELECT revision_no+1 FROM import_item_source_snapshots WHERE id=NEW.base_source_snapshot_id
  )
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.result_source_snapshot_id
  AND validation.prepublish_generation=4
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment result'); END;
CREATE TRIGGER review_multidisc_attachment_terminal_update
BEFORE UPDATE ON review_multidisc_attachments
WHEN OLD.state IN ('ACCEPTED','REJECTED','CANCELLED')
BEGIN SELECT RAISE(ABORT,'terminal multi-disc attachment is immutable'); END;
CREATE TRIGGER review_multidisc_attachment_delete
BEFORE DELETE ON review_multidisc_attachments BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER save_states_disc_insert
BEFORE INSERT ON save_states
WHEN NEW.source_launch_session_id IS NULL AND NEW.disc_index IS NOT NULL
OR NEW.source_launch_session_id IS NOT NULL AND (
  EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND (
    NEW.disc_index IS NULL OR NEW.disc_index >= (
      SELECT count(*) FROM launch_external_files external
      WHERE external.launch_session_id=NEW.source_launch_session_id AND external.kind='DISC'
    )
  )
  OR NOT EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND NEW.disc_index IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'save state disc index mismatch'); END;
CREATE TRIGGER save_states_disc_update
BEFORE UPDATE OF source_launch_session_id,disc_index ON save_states
WHEN NEW.source_launch_session_id IS NULL AND NEW.disc_index IS NOT NULL
OR NEW.source_launch_session_id IS NOT NULL AND (
  EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND (
    NEW.disc_index IS NULL OR NEW.disc_index >= (
      SELECT count(*) FROM launch_external_files external
      WHERE external.launch_session_id=NEW.source_launch_session_id AND external.kind='DISC'
    )
  )
  OR NOT EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND NEW.disc_index IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'save state disc index mismatch'); END;
