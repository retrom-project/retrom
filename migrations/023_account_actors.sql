CREATE TABLE audit_events_v23 (
  id TEXT PRIMARY KEY,
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT REFERENCES users(id),
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','offline-recovery','startup-test-bootstrap','restore-security-fence'
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
INSERT INTO audit_events_v23(
  id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
  before_json,after_json,diff_json,request_id,created_at_ms
)
SELECT id,'SYSTEM',NULL,'release-setup',action,resource_type,resource_id,
       before_json,after_json,diff_json,request_id,created_at_ms
FROM audit_events;
DROP TABLE audit_events;
ALTER TABLE audit_events_v23 RENAME TO audit_events;
CREATE INDEX audit_events_resource ON audit_events(resource_type,resource_id,created_at_ms,id);
CREATE INDEX audit_events_actor ON audit_events(actor_user_id,created_at_ms,id);
CREATE TRIGGER audit_events_immutable_update
BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER audit_events_immutable_delete
BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TABLE review_events_v23 (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  event_type TEXT NOT NULL CHECK(event_type IN (
    'DRAFT_SAVED','TARGET_CHANGED','SCRAPE_REQUESTED','CANDIDATE_APPLIED','CANDIDATE_REMOVED',
    'PARENT_UPLOAD_REQUESTED','PARENT_ATTACHMENT_ACCEPTED','PARENT_ATTACHMENT_REJECTED','APPROVED','DISCARDED'
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
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);
INSERT INTO review_events_v23(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,after_json,
  diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms
)
SELECT id,import_item_id,event_type,'SYSTEM',NULL,'release-setup',before_json,after_json,
       diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms
FROM review_events;
DROP TABLE review_events;
ALTER TABLE review_events_v23 RENAME TO review_events;
CREATE INDEX review_events_history ON review_events(event_type,created_at_ms,id);
CREATE INDEX review_events_actor ON review_events(actor_user_id,created_at_ms,id);
CREATE TRIGGER review_events_immutable_update
BEFORE UPDATE ON review_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER review_events_immutable_delete
BEFORE DELETE ON review_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TABLE import_job_file_resolutions_v23 (
  import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  upload_file_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('RECONFIGURED')),
  replacement_import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT REFERENCES users(id),
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','offline-recovery','startup-test-bootstrap','restore-security-fence'
  )),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_job_id,upload_file_id),
  FOREIGN KEY(import_job_id,upload_file_id) REFERENCES import_job_files(import_job_id,upload_file_id),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);
INSERT INTO import_job_file_resolutions_v23(
  import_job_id,upload_file_id,action,replacement_import_job_id,
  actor_kind,actor_user_id,actor_label,created_at_ms
)
SELECT import_job_id,upload_file_id,action,replacement_import_job_id,
       'SYSTEM',NULL,'release-setup',created_at_ms
FROM import_job_file_resolutions;
DROP TABLE import_job_file_resolutions;
ALTER TABLE import_job_file_resolutions_v23 RENAME TO import_job_file_resolutions;
CREATE INDEX fk_import_job_file_resolutions_replacement
ON import_job_file_resolutions(replacement_import_job_id);
CREATE INDEX import_job_file_resolutions_actor
ON import_job_file_resolutions(actor_user_id,created_at_ms);
CREATE TRIGGER import_job_file_resolutions_immutable_update
BEFORE UPDATE ON import_job_file_resolutions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_job_file_resolutions_immutable_delete
BEFORE DELETE ON import_job_file_resolutions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
