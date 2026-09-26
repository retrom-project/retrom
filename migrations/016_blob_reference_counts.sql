-- Keep the effective owner count in the same transaction as every reference write.
-- Launch references and GC bookkeeping do not own payloads.

CREATE TRIGGER count_import_files_blob_id_insert AFTER INSERT ON import_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_files_blob_id_delete AFTER DELETE ON import_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_import_files_blob_id_update AFTER UPDATE OF blob_id ON import_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_bios_installations_blob_id_insert AFTER INSERT ON bios_installations
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_bios_installations_blob_id_delete AFTER DELETE ON bios_installations
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_bios_installations_blob_id_update AFTER UPDATE OF blob_id ON bios_installations
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_content_hash_evidence_archive_blob_id_insert AFTER INSERT ON content_hash_evidence
WHEN NEW.archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.archive_blob_id;
END;

CREATE TRIGGER count_content_hash_evidence_archive_blob_id_delete AFTER DELETE ON content_hash_evidence
WHEN OLD.archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.archive_blob_id;
END;

CREATE TRIGGER count_content_hash_evidence_archive_blob_id_update AFTER UPDATE OF archive_blob_id ON content_hash_evidence
WHEN OLD.archive_blob_id IS NOT NEW.archive_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.archive_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.archive_blob_id;
END;

CREATE TRIGGER count_content_hash_evidence_blob_id_insert AFTER INSERT ON content_hash_evidence
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_content_hash_evidence_blob_id_delete AFTER DELETE ON content_hash_evidence
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_content_hash_evidence_blob_id_update AFTER UPDATE OF blob_id ON content_hash_evidence
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_game_assets_blob_id_insert AFTER INSERT ON game_assets
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_game_assets_blob_id_delete AFTER DELETE ON game_assets
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_game_assets_blob_id_update AFTER UPDATE OF blob_id ON game_assets
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_game_files_blob_id_insert AFTER INSERT ON game_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_game_files_blob_id_delete AFTER DELETE ON game_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_game_files_blob_id_update AFTER UPDATE OF blob_id ON game_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_game_files_source_archive_blob_id_insert AFTER INSERT ON game_files
WHEN NEW.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_game_files_source_archive_blob_id_delete AFTER DELETE ON game_files
WHEN OLD.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
END;

CREATE TRIGGER count_game_files_source_archive_blob_id_update AFTER UPDATE OF source_archive_blob_id ON game_files
WHEN OLD.source_archive_blob_id IS NOT NEW.source_archive_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_source_files_blob_id_insert AFTER INSERT ON import_item_source_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_source_files_blob_id_delete AFTER DELETE ON import_item_source_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_import_item_source_files_blob_id_update AFTER UPDATE OF blob_id ON import_item_source_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_source_files_source_archive_blob_id_insert AFTER INSERT ON import_item_source_files
WHEN NEW.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_source_files_source_archive_blob_id_delete AFTER DELETE ON import_item_source_files
WHEN OLD.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_source_files_source_archive_blob_id_update AFTER UPDATE OF source_archive_blob_id ON import_item_source_files
WHEN OLD.source_archive_blob_id IS NOT NEW.source_archive_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_source_snapshot_files_blob_id_insert AFTER INSERT ON import_item_source_snapshot_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_source_snapshot_files_blob_id_delete AFTER DELETE ON import_item_source_snapshot_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_import_item_source_snapshot_files_blob_id_update AFTER UPDATE OF blob_id ON import_item_source_snapshot_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_source_snapshot_files_source_archive_blob_id_insert AFTER INSERT ON import_item_source_snapshot_files
WHEN NEW.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_source_snapshot_files_source_archive_blob_id_delete AFTER DELETE ON import_item_source_snapshot_files
WHEN OLD.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_source_snapshot_files_source_archive_blob_id_update AFTER UPDATE OF source_archive_blob_id ON import_item_source_snapshot_files
WHEN OLD.source_archive_blob_id IS NOT NEW.source_archive_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_import_item_multidisc_entries_blob_id_insert AFTER INSERT ON import_item_multidisc_entries
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_multidisc_entries_blob_id_delete AFTER DELETE ON import_item_multidisc_entries
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_import_item_multidisc_entries_blob_id_update AFTER UPDATE OF blob_id ON import_item_multidisc_entries
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_validation_files_blob_id_insert AFTER INSERT ON import_item_validation_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_import_item_validation_files_blob_id_delete AFTER DELETE ON import_item_validation_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_import_item_validation_files_blob_id_update AFTER UPDATE OF blob_id ON import_item_validation_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_review_arcade_parent_attachments_accepted_blob_id_insert AFTER INSERT ON review_arcade_parent_attachments
WHEN NEW.accepted_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.accepted_blob_id;
END;

CREATE TRIGGER count_review_arcade_parent_attachments_accepted_blob_id_delete AFTER DELETE ON review_arcade_parent_attachments
WHEN OLD.accepted_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.accepted_blob_id;
END;

CREATE TRIGGER count_review_arcade_parent_attachments_accepted_blob_id_update AFTER UPDATE OF accepted_blob_id ON review_arcade_parent_attachments
WHEN OLD.accepted_blob_id IS NOT NEW.accepted_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.accepted_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.accepted_blob_id;
END;

CREATE TRIGGER count_metadata_provider_responses_raw_response_blob_id_insert AFTER INSERT ON metadata_provider_responses
WHEN NEW.raw_response_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.raw_response_blob_id;
END;

CREATE TRIGGER count_metadata_provider_responses_raw_response_blob_id_delete AFTER DELETE ON metadata_provider_responses
WHEN OLD.raw_response_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.raw_response_blob_id;
END;

CREATE TRIGGER count_metadata_provider_responses_raw_response_blob_id_update AFTER UPDATE OF raw_response_blob_id ON metadata_provider_responses
WHEN OLD.raw_response_blob_id IS NOT NEW.raw_response_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.raw_response_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.raw_response_blob_id;
END;

CREATE TRIGGER count_source_import_item_assets_blob_id_insert AFTER INSERT ON source_import_item_assets
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_source_import_item_assets_blob_id_delete AFTER DELETE ON source_import_item_assets
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_source_import_item_assets_blob_id_update AFTER UPDATE OF blob_id ON source_import_item_assets
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_source_import_item_files_blob_id_insert AFTER INSERT ON source_import_item_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_source_import_item_files_blob_id_delete AFTER DELETE ON source_import_item_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_source_import_item_files_blob_id_update AFTER UPDATE OF blob_id ON source_import_item_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_source_import_item_files_source_archive_blob_id_insert AFTER INSERT ON source_import_item_files
WHEN NEW.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_source_import_item_files_source_archive_blob_id_delete AFTER DELETE ON source_import_item_files
WHEN OLD.source_archive_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
END;

CREATE TRIGGER count_source_import_item_files_source_archive_blob_id_update AFTER UPDATE OF source_archive_blob_id ON source_import_item_files
WHEN OLD.source_archive_blob_id IS NOT NEW.source_archive_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.source_archive_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.source_archive_blob_id;
END;

CREATE TRIGGER count_review_uploaded_assets_blob_id_insert AFTER INSERT ON review_uploaded_assets
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_review_uploaded_assets_blob_id_delete AFTER DELETE ON review_uploaded_assets
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_review_uploaded_assets_blob_id_update AFTER UPDATE OF blob_id ON review_uploaded_assets
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_content_blob_id_insert AFTER INSERT ON review_preview_sessions
WHEN NEW.content_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.content_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_content_blob_id_delete AFTER DELETE ON review_preview_sessions
WHEN OLD.content_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.content_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_content_blob_id_update AFTER UPDATE OF content_blob_id ON review_preview_sessions
WHEN OLD.content_blob_id IS NOT NEW.content_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.content_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.content_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_checkpoint_payload_blob_id_insert AFTER INSERT ON review_preview_sessions
WHEN NEW.checkpoint_payload_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.checkpoint_payload_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_checkpoint_payload_blob_id_delete AFTER DELETE ON review_preview_sessions
WHEN OLD.checkpoint_payload_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.checkpoint_payload_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_checkpoint_payload_blob_id_update AFTER UPDATE OF checkpoint_payload_blob_id ON review_preview_sessions
WHEN OLD.checkpoint_payload_blob_id IS NOT NEW.checkpoint_payload_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.checkpoint_payload_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.checkpoint_payload_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_restore_payload_blob_id_insert AFTER INSERT ON review_preview_sessions
WHEN NEW.restore_payload_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.restore_payload_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_restore_payload_blob_id_delete AFTER DELETE ON review_preview_sessions
WHEN OLD.restore_payload_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.restore_payload_blob_id;
END;

CREATE TRIGGER count_review_preview_sessions_restore_payload_blob_id_update AFTER UPDATE OF restore_payload_blob_id ON review_preview_sessions
WHEN OLD.restore_payload_blob_id IS NOT NEW.restore_payload_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.restore_payload_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.restore_payload_blob_id;
END;

CREATE TRIGGER count_review_preview_files_blob_id_insert AFTER INSERT ON review_preview_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_review_preview_files_blob_id_delete AFTER DELETE ON review_preview_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_review_preview_files_blob_id_update AFTER UPDATE OF blob_id ON review_preview_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_review_runtime_screenshots_blob_id_insert AFTER INSERT ON review_runtime_screenshots
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_review_runtime_screenshots_blob_id_delete AFTER DELETE ON review_runtime_screenshots
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_review_runtime_screenshots_blob_id_update AFTER UPDATE OF blob_id ON review_runtime_screenshots
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_save_states_payload_blob_id_insert AFTER INSERT ON save_states
WHEN NEW.payload_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.payload_blob_id;
END;

CREATE TRIGGER count_save_states_payload_blob_id_delete AFTER DELETE ON save_states
WHEN OLD.payload_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.payload_blob_id;
END;

CREATE TRIGGER count_save_states_payload_blob_id_update AFTER UPDATE OF payload_blob_id ON save_states
WHEN OLD.payload_blob_id IS NOT NEW.payload_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.payload_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.payload_blob_id;
END;

CREATE TRIGGER count_save_states_screenshot_blob_id_insert AFTER INSERT ON save_states
WHEN NEW.screenshot_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.screenshot_blob_id;
END;

CREATE TRIGGER count_save_states_screenshot_blob_id_delete AFTER DELETE ON save_states
WHEN OLD.screenshot_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.screenshot_blob_id;
END;

CREATE TRIGGER count_save_states_screenshot_blob_id_update AFTER UPDATE OF screenshot_blob_id ON save_states
WHEN OLD.screenshot_blob_id IS NOT NEW.screenshot_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.screenshot_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.screenshot_blob_id;
END;

CREATE TRIGGER count_scrape_candidate_assets_blob_id_insert AFTER INSERT ON scrape_candidate_assets
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_scrape_candidate_assets_blob_id_delete AFTER DELETE ON scrape_candidate_assets
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_scrape_candidate_assets_blob_id_update AFTER UPDATE OF blob_id ON scrape_candidate_assets
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_upload_files_final_blob_id_insert AFTER INSERT ON upload_files
WHEN NEW.final_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.final_blob_id;
END;

CREATE TRIGGER count_upload_files_final_blob_id_delete AFTER DELETE ON upload_files
WHEN OLD.final_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.final_blob_id;
END;

CREATE TRIGGER count_upload_files_final_blob_id_update AFTER UPDATE OF final_blob_id ON upload_files
WHEN OLD.final_blob_id IS NOT NEW.final_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.final_blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.final_blob_id;
END;

CREATE TRIGGER count_variant_files_blob_id_insert AFTER INSERT ON variant_files
WHEN NEW.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

CREATE TRIGGER count_variant_files_blob_id_delete AFTER DELETE ON variant_files
WHEN OLD.blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
END;

CREATE TRIGGER count_variant_files_blob_id_update AFTER UPDATE OF blob_id ON variant_files
WHEN OLD.blob_id IS NOT NEW.blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.blob_id;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.blob_id;
END;

-- A referenced archive owns each materialized member exactly once per entry.
-- Direct owner counts greater than one still contribute only one archive membership.
CREATE TRIGGER count_archive_members_add AFTER UPDATE OF ref_count ON blobs
WHEN OLD.ref_count=0 AND NEW.ref_count>0 BEGIN
  UPDATE blobs SET ref_count=ref_count+(
    SELECT count(*) FROM archive_entries entry
    WHERE entry.archive_blob_id=NEW.id AND entry.materialized_blob_id=blobs.id
  ) WHERE id IN (SELECT materialized_blob_id FROM archive_entries
    WHERE archive_blob_id=NEW.id AND materialized_blob_id IS NOT NULL);
END;

CREATE TRIGGER count_archive_members_remove AFTER UPDATE OF ref_count ON blobs
WHEN OLD.ref_count>0 AND NEW.ref_count=0 BEGIN
  UPDATE blobs SET ref_count=ref_count-(
    SELECT count(*) FROM archive_entries entry
    WHERE entry.archive_blob_id=NEW.id AND entry.materialized_blob_id=blobs.id
  ) WHERE id IN (SELECT materialized_blob_id FROM archive_entries
    WHERE archive_blob_id=NEW.id AND materialized_blob_id IS NOT NULL);
END;

CREATE TRIGGER count_archive_entry_insert AFTER INSERT ON archive_entries
WHEN NEW.materialized_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.materialized_blob_id
    AND (SELECT ref_count FROM blobs WHERE id=NEW.archive_blob_id)>0;
END;

CREATE TRIGGER count_archive_entry_delete AFTER DELETE ON archive_entries
WHEN OLD.materialized_blob_id IS NOT NULL BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.materialized_blob_id
    AND (SELECT ref_count FROM blobs WHERE id=OLD.archive_blob_id)>0;
END;

CREATE TRIGGER count_archive_entry_update AFTER UPDATE OF materialized_blob_id,archive_blob_id ON archive_entries
WHEN OLD.materialized_blob_id IS NOT NEW.materialized_blob_id OR OLD.archive_blob_id IS NOT NEW.archive_blob_id BEGIN
  UPDATE blobs SET ref_count=ref_count-1 WHERE id=OLD.materialized_blob_id
    AND (SELECT ref_count FROM blobs WHERE id=OLD.archive_blob_id)>0;
  UPDATE blobs SET ref_count=ref_count+1 WHERE id=NEW.materialized_blob_id
    AND (SELECT ref_count FROM blobs WHERE id=NEW.archive_blob_id)>0;
END;
