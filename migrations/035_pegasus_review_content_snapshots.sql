-- Pegasus review publication keeps the Pegasus item as the provenance ref,
-- while the accepted content manifest must follow the review's current
-- effective source snapshot after Parent ROM or multi-disc attachments.

DROP TRIGGER game_content_revisions_pegasus_source_insert;

CREATE TRIGGER game_content_revisions_pegasus_source_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1
  FROM pegasus_import_items item
  WHERE item.id=NEW.source_ref_id
  AND item.content_kind=NEW.content_kind
  AND (
    (
      item.execution_state='PUBLISHING'
      AND item.source_manifest_digest=NEW.source_manifest_digest
    )
    OR
    (
      item.execution_state='REVIEW_PENDING'
      AND item.library_import_item_id IS NOT NULL
      AND EXISTS(
        SELECT 1
        FROM review_drafts draft
        JOIN import_item_source_snapshots snapshot
          ON snapshot.id=draft.effective_source_snapshot_id
        WHERE draft.import_item_id=item.library_import_item_id
        AND snapshot.import_item_id=item.library_import_item_id
        AND snapshot.content_kind=NEW.content_kind
        AND snapshot.source_manifest_digest=NEW.source_manifest_digest
      )
    )
  )
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus content source'); END;
