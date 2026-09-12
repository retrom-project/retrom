-- BIOS replacement preserves immutable running inputs. Retirement uses bounded,
-- indexed work queues instead of traversing dependency JSON in the install transaction.
CREATE INDEX bios_installations_retirement ON bios_installations(updated_at_ms,id)
WHERE is_active=0 AND blob_id IS NOT NULL;
CREATE INDEX variant_files_bios_blob ON variant_files(blob_id,game_variant_id,logical_name)
WHERE role='BIOS_BUNDLE';
CREATE INDEX bios_installations_active_blob ON bios_installations(blob_id) WHERE is_active=1;

-- Session writes maintain retirement deadlines in the same transaction.
CREATE TABLE launch_payload_retirements (
 launch_session_id TEXT PRIMARY KEY REFERENCES launch_sessions(id) ON DELETE CASCADE,
 due_at_ms INTEGER NOT NULL CHECK(due_at_ms>=0),
 released_at_ms INTEGER CHECK(released_at_ms IS NULL OR released_at_ms>=0)
);
CREATE INDEX launch_payload_retirement ON launch_payload_retirements(due_at_ms,launch_session_id)
WHERE released_at_ms IS NULL;
