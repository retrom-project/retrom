CREATE TABLE review_uploaded_assets (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  upload_file_id TEXT NOT NULL UNIQUE REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  kind TEXT NOT NULL CHECK(kind = 'COVER'),
  width_px INTEGER NOT NULL CHECK(width_px > 0),
  height_px INTEGER NOT NULL CHECK(height_px > 0),
  media_type TEXT NOT NULL CHECK(media_type IN ('image/png','image/jpeg','image/webp')),
  created_at_ms INTEGER NOT NULL
);

CREATE TABLE upload_consumptions_v2 (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  consumer_type TEXT NOT NULL CHECK(consumer_type IN ('IMPORT_JOB','GAME_FILE_REVISION_JOB','GAME_ASSET','REVIEW_ASSET','BIOS_INSTALLATION','DAT_VERSION')),
  consumer_id TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(consumer_type, consumer_id)
);

INSERT INTO upload_consumptions_v2(id, upload_session_id, upload_file_id, consumer_type, consumer_id, created_at_ms)
SELECT id, upload_session_id, upload_file_id, consumer_type, consumer_id, created_at_ms
FROM upload_consumptions;

DROP TABLE upload_consumptions;
ALTER TABLE upload_consumptions_v2 RENAME TO upload_consumptions;
CREATE UNIQUE INDEX upload_consumptions_whole_session ON upload_consumptions(upload_session_id) WHERE upload_file_id IS NULL;

ALTER TABLE review_drafts
ADD COLUMN cover_uploaded_asset_id TEXT REFERENCES review_uploaded_assets(id);

CREATE INDEX review_uploaded_assets_item ON review_uploaded_assets(import_item_id, created_at_ms, id);

CREATE TRIGGER review_uploaded_assets_immutable_update BEFORE UPDATE ON review_uploaded_assets
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER review_uploaded_assets_immutable_delete BEFORE DELETE ON review_uploaded_assets
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER review_drafts_uploaded_cover_insert BEFORE INSERT ON review_drafts
WHEN NEW.cover_uploaded_asset_id IS NOT NULL
AND (
  NEW.cover_candidate_asset_id IS NOT NULL
  OR NOT EXISTS (
    SELECT 1 FROM review_uploaded_assets a
    WHERE a.id=NEW.cover_uploaded_asset_id
    AND a.import_item_id=NEW.import_item_id
    AND a.kind='COVER'
  )
)
BEGIN SELECT RAISE(ABORT, 'invalid review uploaded cover'); END;

CREATE TRIGGER review_drafts_uploaded_cover_update BEFORE UPDATE OF import_item_id,cover_candidate_asset_id,cover_uploaded_asset_id ON review_drafts
WHEN NEW.cover_uploaded_asset_id IS NOT NULL
AND (
  NEW.cover_candidate_asset_id IS NOT NULL
  OR NOT EXISTS (
    SELECT 1 FROM review_uploaded_assets a
    WHERE a.id=NEW.cover_uploaded_asset_id
    AND a.import_item_id=NEW.import_item_id
    AND a.kind='COVER'
  )
)
BEGIN SELECT RAISE(ABORT, 'invalid review uploaded cover'); END;
