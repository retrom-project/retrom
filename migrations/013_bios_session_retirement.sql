-- BIOS replacement preserves immutable running inputs. Retirement uses bounded,
-- indexed work queues instead of traversing dependency JSON in the install transaction.
CREATE INDEX bios_installations_retirement ON bios_installations(updated_at_ms,id)
WHERE is_active=0 AND blob_id IS NOT NULL;
CREATE INDEX variant_files_bios_blob ON variant_files(blob_id,game_variant_id,logical_name)
WHERE role='BIOS_BUNDLE';
CREATE INDEX bios_installations_active_blob ON bios_installations(blob_id) WHERE is_active=1;

-- A lifecycle side table keeps the frozen launch schema unchanged.
CREATE TABLE launch_payload_retirements (
 launch_session_id TEXT PRIMARY KEY REFERENCES launch_sessions(id) ON DELETE CASCADE,
 due_at_ms INTEGER NOT NULL CHECK(due_at_ms>=0),
 released_at_ms INTEGER CHECK(released_at_ms IS NULL OR released_at_ms>=0)
);
CREATE INDEX launch_payload_retirement ON launch_payload_retirements(due_at_ms,launch_session_id)
WHERE released_at_ms IS NULL;
INSERT INTO launch_payload_retirements(launch_session_id,due_at_ms)
SELECT launch.id,CASE WHEN launch.state IN ('FINISHED','EXPIRED','REVOKED') THEN launch.finished_at_ms WHEN launch.state='CREATED' THEN min(launch.bootstrap_expires_at_ms,launch.hard_expires_at_ms) ELSE min(COALESCE(launch.idle_expires_at_ms,launch.hard_expires_at_ms),launch.hard_expires_at_ms) END FROM launch_sessions launch;
CREATE TRIGGER launch_payload_retirement_insert AFTER INSERT ON launch_sessions
BEGIN
 INSERT INTO launch_payload_retirements(launch_session_id,due_at_ms) VALUES(NEW.id,CASE WHEN NEW.state IN ('FINISHED','EXPIRED','REVOKED') THEN NEW.finished_at_ms WHEN NEW.state='CREATED' THEN min(NEW.bootstrap_expires_at_ms,NEW.hard_expires_at_ms) ELSE min(COALESCE(NEW.idle_expires_at_ms,NEW.hard_expires_at_ms),NEW.hard_expires_at_ms) END);
END;
CREATE TRIGGER launch_payload_retirement_update AFTER UPDATE OF state,bootstrap_expires_at_ms,hard_expires_at_ms,idle_expires_at_ms,finished_at_ms ON launch_sessions
BEGIN
 UPDATE launch_payload_retirements SET due_at_ms=CASE WHEN NEW.state IN ('FINISHED','EXPIRED','REVOKED') THEN NEW.finished_at_ms WHEN NEW.state='CREATED' THEN min(NEW.bootstrap_expires_at_ms,NEW.hard_expires_at_ms) ELSE min(COALESCE(NEW.idle_expires_at_ms,NEW.hard_expires_at_ms),NEW.hard_expires_at_ms) END WHERE launch_session_id=NEW.id AND released_at_ms IS NULL;
END;
