-- Play statistics no longer determine when a product launch can read content or save.
UPDATE launch_sessions SET idle_expires_at_ms=NULL
WHERE state='ACTIVE' AND idle_expires_at_ms IS NOT NULL;

UPDATE launch_payload_retirements SET due_at_ms=(
  SELECT hard_expires_at_ms FROM launch_sessions WHERE id=launch_session_id
)
WHERE released_at_ms IS NULL AND launch_session_id IN (
  SELECT id FROM launch_sessions WHERE state='ACTIVE'
);
