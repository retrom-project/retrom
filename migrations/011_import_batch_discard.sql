-- One durable disposition per import batch; payload deletion stays in PAYLOAD_RELEASE/GC.
CREATE TABLE import_batch_discards (
  kind TEXT NOT NULL CHECK(kind IN ('IMPORT','PEGASUS','EMULATIONSTATION')),
  import_id TEXT NOT NULL,
  requested_by_user_id TEXT NOT NULL REFERENCES users(id),
  state TEXT NOT NULL CHECK(state IN ('REQUESTED','COMPLETED','FAILED')),
  error_code TEXT,
  requested_at_ms INTEGER NOT NULL CHECK(requested_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=requested_at_ms),
  completed_at_ms INTEGER,
  PRIMARY KEY(kind,import_id),
  CHECK((state='COMPLETED')=(completed_at_ms IS NOT NULL)),
  CHECK((state='FAILED')=(error_code IS NOT NULL))
);

CREATE TABLE server_import_upload_owners (
 upload_session_id TEXT PRIMARY KEY REFERENCES upload_sessions(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('PEGASUS','EMULATIONSTATION')),
 source_item_id TEXT NOT NULL,
 UNIQUE(kind,source_item_id)
);

CREATE VIEW discarded_import_jobs AS
SELECT import_id FROM import_batch_discards WHERE kind='IMPORT'
UNION
SELECT item.library_import_job_id FROM pegasus_import_items item
JOIN import_batch_discards batch ON batch.kind='PEGASUS' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT item.library_import_job_id FROM emulationstation_import_items item
JOIN import_batch_discards batch ON batch.kind='EMULATIONSTATION' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON owner.upload_session_id=job.upload_session_id
JOIN pegasus_import_items item ON owner.kind='PEGASUS' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON owner.upload_session_id=job.upload_session_id
JOIN emulationstation_import_items item ON owner.kind='EMULATIONSTATION' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id;

CREATE TRIGGER discarded_import_publication_fence BEFORE UPDATE OF state ON import_items
WHEN NEW.state='PUBLISHED' AND EXISTS(SELECT 1 FROM discarded_import_jobs WHERE import_id=NEW.import_job_id)
BEGIN SELECT RAISE(ABORT,'IMPORT_BATCH_DISCARDED'); END;

CREATE TRIGGER discarded_import_retry_fence BEFORE UPDATE OF state ON import_items
WHEN NEW.state='QUEUED' AND OLD.state<>'QUEUED'
AND EXISTS(SELECT 1 FROM discarded_import_jobs WHERE import_id=NEW.import_job_id)
BEGIN SELECT RAISE(ABORT,'IMPORT_BATCH_DISCARDED'); END;

CREATE TRIGGER discarded_pegasus_retry_fence BEFORE UPDATE OF state ON pegasus_imports
WHEN NEW.state='QUEUED' AND EXISTS(
  SELECT 1 FROM import_batch_discards WHERE kind='PEGASUS' AND import_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'IMPORT_BATCH_DISCARDED'); END;

CREATE TRIGGER discarded_emulationstation_retry_fence BEFORE UPDATE OF state ON emulationstation_imports
WHEN NEW.state='QUEUED' AND EXISTS(
  SELECT 1 FROM import_batch_discards WHERE kind='EMULATIONSTATION' AND import_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'IMPORT_BATCH_DISCARDED'); END;

DROP TRIGGER pegasus_item_review_discarded_update;
CREATE TRIGGER pegasus_item_review_discarded_update
BEFORE UPDATE OF execution_state ON pegasus_import_items
WHEN NEW.execution_state='REVIEW_DISCARDED'
AND NOT EXISTS(SELECT 1 FROM import_batch_discards batch
 WHERE batch.kind='PEGASUS' AND batch.import_id=NEW.import_id
 AND (NEW.library_import_item_id IS NULL OR EXISTS(SELECT 1 FROM import_items item
 WHERE item.id=NEW.library_import_item_id AND item.state IN ('DISCARDED','FAILED_FINAL','CANCELLED'))))
AND NOT EXISTS(
  SELECT 1 FROM import_items item WHERE item.id=NEW.library_import_item_id AND item.state='DISCARDED'
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus review discard'); END;

DROP TRIGGER emulationstation_item_review_discarded_update;
CREATE TRIGGER emulationstation_item_review_discarded_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN NEW.execution_state='REVIEW_DISCARDED'
AND NOT EXISTS(SELECT 1 FROM import_batch_discards batch
 WHERE batch.kind='EMULATIONSTATION' AND batch.import_id=NEW.import_id
 AND (NEW.library_import_item_id IS NULL OR EXISTS(SELECT 1 FROM import_items item
 WHERE item.id=NEW.library_import_item_id AND item.state IN ('DISCARDED','FAILED_FINAL','CANCELLED'))))
AND NOT EXISTS(
  SELECT 1 FROM import_items item
  JOIN review_events event ON event.import_item_id=item.id AND event.event_type='DISCARDED'
  WHERE item.id=NEW.library_import_item_id AND item.state='DISCARDED'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation review discard'); END;

DROP TRIGGER emulationstation_item_execution_update;
CREATE TRIGGER emulationstation_item_execution_update
BEFORE UPDATE OF execution_state,error_code,retryable,library_import_job_id,library_import_item_id,
  published_game_id,existing_game_id,error_details_json,completed_at_ms
ON emulationstation_import_items
WHEN (NEW.library_import_job_id IS NULL)<>(NEW.library_import_item_id IS NULL)
  OR NEW.execution_state='PUBLISHED' AND (NEW.published_game_id IS NULL OR NEW.existing_game_id IS NOT NULL)
  OR NEW.execution_state<>'PUBLISHED' AND NEW.published_game_id IS NOT NULL
  OR NEW.execution_state='SKIPPED_EXISTING' AND NEW.existing_game_id IS NULL
  OR NEW.execution_state<>'SKIPPED_EXISTING' AND NEW.existing_game_id IS NOT NULL
  OR NEW.retryable=1 AND NEW.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
  OR NEW.error_details_json IS NOT NULL AND NEW.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
 AND NOT (NEW.execution_state='REVIEW_DISCARDED' AND EXISTS(SELECT 1 FROM import_batch_discards
 WHERE kind='EMULATIONSTATION' AND import_id=NEW.import_id))
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item execution'); END;

DROP TRIGGER emulationstation_item_state_update;
CREATE TRIGGER emulationstation_item_state_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN OLD.execution_state<>NEW.execution_state AND NOT (
 NEW.execution_state='REVIEW_DISCARDED' AND OLD.execution_state NOT IN ('PUBLISHED','SKIPPED_EXISTING')
 AND EXISTS(SELECT 1 FROM import_batch_discards WHERE kind='EMULATIONSTATION' AND import_id=NEW.import_id) OR
  OLD.execution_state='PENDING' AND NEW.execution_state IN ('COPYING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='COPYING' AND NEW.execution_state IN ('VALIDATING','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='VALIDATING' AND NEW.execution_state IN ('REVIEW_PENDING','SKIPPED_EXISTING','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='REVIEW_PENDING' AND NEW.execution_state IN ('PUBLISHED','REVIEW_DISCARDED') OR
  OLD.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND OLD.retryable=1 AND NEW.execution_state='PENDING'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item state transition'); END;

DROP TRIGGER emulationstation_import_state_update;
CREATE TRIGGER emulationstation_import_state_update
BEFORE UPDATE OF state ON emulationstation_imports
WHEN OLD.state<>NEW.state AND NOT (
 NEW.state='COMPLETED' AND OLD.state IN ('PARTIAL_FAILURE','FAILED','CANCELLED')
 AND EXISTS(SELECT 1 FROM import_batch_discards WHERE kind='EMULATIONSTATION' AND import_id=NEW.id) OR
  OLD.state='SCANNING' AND NEW.state IN ('AWAITING_MAPPING','FAILED') OR
  OLD.state='AWAITING_MAPPING' AND NEW.state IN ('QUEUED','EXPIRED','FAILED') OR
  OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELLED','FAILED') OR
  OLD.state='RUNNING' AND NEW.state IN ('QUEUED','COMPLETED','PARTIAL_FAILURE','CANCEL_REQUESTED','CANCELLED','FAILED') OR
  OLD.state='CANCEL_REQUESTED' AND NEW.state IN ('CANCELLED','FAILED') OR
  OLD.state IN ('PARTIAL_FAILURE','FAILED') AND OLD.retryable=1 AND NEW.state='QUEUED'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation import state transition'); END;
