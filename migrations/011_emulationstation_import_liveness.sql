-- Content identification can reject an item before it owns a library review.
-- That path closes the source item directly from COPYING as BLOCKED_CONTENT.

DROP TRIGGER emulationstation_item_state_update;

CREATE TRIGGER emulationstation_item_state_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN OLD.execution_state<>NEW.execution_state AND NOT (
  OLD.execution_state='PENDING' AND NEW.execution_state IN ('COPYING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='COPYING' AND NEW.execution_state IN ('VALIDATING','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='VALIDATING' AND NEW.execution_state IN ('REVIEW_PENDING','SKIPPED_EXISTING','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='REVIEW_PENDING' AND NEW.execution_state IN ('PUBLISHED','REVIEW_DISCARDED') OR
  OLD.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND OLD.retryable=1 AND NEW.execution_state='PENDING'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item state transition'); END;
