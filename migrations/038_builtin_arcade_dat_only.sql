-- Retire administrator-supplied Arcade DAT candidates. Historical USER rows
-- remain available to immutable revisions, but can no longer be changed or
-- activated. New catalog versions are materialized only from the release
-- dependency manifest.

UPDATE jobs
SET state='CANCELLED',
    finished_at_ms=updated_at_ms,
    cancel_requested_at_ms=updated_at_ms,
    cancel_reason='USER_DAT_RETIRED',
    leased_until_ms=NULL,
    worker_id=NULL,
    version=version+1
WHERE kind='DAT_PARSE'
  AND scope_type='DAT_VERSION'
  AND scope_id IN (SELECT id FROM dat_versions WHERE source='USER')
  AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

UPDATE dat_versions
SET is_active=0,
    parse_status=CASE WHEN parse_status IN ('PENDING','PARSING') THEN 'CANCELLED' ELSE parse_status END,
    version=version+1
WHERE source='USER'
  AND (is_active=1 OR parse_status IN ('PENDING','PARSING'));

DROP TABLE dat_diff_items;
DROP TABLE dat_diff_snapshots;
DROP TABLE dat_import_jobs;

CREATE TRIGGER dat_versions_builtin_only_insert
BEFORE INSERT ON dat_versions
WHEN NEW.source<>'BUILTIN'
BEGIN
  SELECT RAISE(ABORT,'only release-managed built-in DAT versions are supported');
END;

CREATE TRIGGER dat_versions_legacy_user_read_only
BEFORE UPDATE ON dat_versions
WHEN OLD.source='USER' OR NEW.source<>'BUILTIN'
BEGIN
  SELECT RAISE(ABORT,'legacy user DAT versions are read-only');
END;
