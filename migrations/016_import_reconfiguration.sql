ALTER TABLE import_jobs
ADD COLUMN resolved_rejected_file_count INTEGER NOT NULL DEFAULT 0
CHECK(resolved_rejected_file_count BETWEEN 0 AND rejected_file_count);

ALTER TABLE import_jobs
ADD COLUMN reconfigured_from_import_job_id TEXT REFERENCES import_jobs(id);

CREATE INDEX fk_import_jobs_reconfigured_from
ON import_jobs(reconfigured_from_import_job_id);

CREATE TABLE import_job_file_resolutions (
  import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  upload_file_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('RECONFIGURED')),
  replacement_import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  actor TEXT NOT NULL CHECK(actor = 'local'),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_job_id, upload_file_id),
  FOREIGN KEY(import_job_id, upload_file_id) REFERENCES import_job_files(import_job_id, upload_file_id)
);

CREATE INDEX fk_import_job_file_resolutions_replacement
ON import_job_file_resolutions(replacement_import_job_id);

CREATE TRIGGER import_job_file_resolutions_immutable_update
BEFORE UPDATE ON import_job_file_resolutions
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER import_job_file_resolutions_immutable_delete
BEFORE DELETE ON import_job_file_resolutions
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;
