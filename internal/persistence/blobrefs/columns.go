package blobrefs

// columns is schema metadata, not an owner list. Each domain explicitly selects its mutation scope.
var columns = map[string]string{
	"bios_installations": "blob_id",

	"content_hash_evidence": "archive_blob_id,blob_id",

	"game_assets": "blob_id",

	"game_files": "blob_id,source_archive_blob_id",

	"import_item_assets": "blob_id",

	"import_files": "blob_id",

	"import_item_multidisc_entries": "blob_id",

	"import_item_source_files": "blob_id,source_archive_blob_id",

	"import_item_source_snapshot_files": "blob_id,source_archive_blob_id",

	"import_item_validation_files": "blob_id",

	"metadata_provider_responses": "raw_response_blob_id",

	"review_arcade_parent_attachments": "accepted_blob_id",

	"review_preview_files": "blob_id",

	"review_preview_sessions": "content_blob_id,checkpoint_payload_blob_id,restore_payload_blob_id",

	"review_runtime_screenshots": "blob_id",

	"review_uploaded_assets": "blob_id",

	"save_states": "payload_blob_id,screenshot_blob_id",

	"scrape_candidate_assets": "blob_id",

	"source_import_item_companions": "blob_id",

	"source_import_item_assets": "blob_id",

	"source_import_item_files": "blob_id,source_archive_blob_id",

	"upload_files": "final_blob_id",

	"variant_files": "blob_id",

	"archive_entries": `CASE WHEN materialized_blob_id<>archive_blob_id AND
	EXISTS(SELECT 1 FROM blobs WHERE id=archive_entries.archive_blob_id AND ref_count>0)
	THEN materialized_blob_id END`,
}
