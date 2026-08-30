-- Browser imports are accepted quickly and prepared by the IMPORT_GROUP worker.

CREATE TABLE import_group_requests (
  import_job_id TEXT PRIMARY KEY REFERENCES import_jobs(id),
  schema_version INTEGER NOT NULL CHECK(schema_version=1),
  request_json TEXT NOT NULL CHECK(json_valid(request_json)),
  request_digest TEXT NOT NULL CHECK(
    length(request_digest)=64 AND request_digest=lower(request_digest)
  ),
  actor_user_id TEXT REFERENCES users(id),
  upload_version INTEGER NOT NULL CHECK(upload_version>=1),
  upload_manifest_digest TEXT NOT NULL CHECK(
    length(upload_manifest_digest)=64 AND upload_manifest_digest=lower(upload_manifest_digest)
  ),
  target_snapshot_json TEXT NOT NULL CHECK(json_valid(target_snapshot_json)),
  target_snapshot_digest TEXT NOT NULL CHECK(
    length(target_snapshot_digest)=64 AND target_snapshot_digest=lower(target_snapshot_digest)
  ),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)
);

CREATE INDEX import_group_requests_actor ON import_group_requests(actor_user_id);

CREATE TRIGGER import_group_requests_immutable_update
BEFORE UPDATE ON import_group_requests
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_group_requests_immutable_delete
BEFORE DELETE ON import_group_requests
BEGIN SELECT RAISE(ABORT,'immutable'); END;
