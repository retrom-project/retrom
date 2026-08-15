-- Instance-wide administrator-managed game tags.

CREATE TABLE tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 40 AND length(CAST(name AS BLOB))<=160),
  name_key TEXT NOT NULL CHECK(length(name_key)>=1 AND length(CAST(name_key AS BLOB))<=160),
  search_text TEXT NOT NULL CHECK(length(search_text)>=1 AND length(CAST(search_text AS BLOB))<=160),
  status TEXT NOT NULL CHECK(status IN ('ACTIVE','DELETED')),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  updated_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  deleted_at_ms INTEGER,
  CHECK((status='DELETED')=(deleted_at_ms IS NOT NULL)),
  CHECK(length(id)=36 AND lower(id)=id
    AND id NOT GLOB '*[^0-9a-f-]*'
    AND substr(id,9,1)='-' AND substr(id,14,1)='-'
    AND substr(id,19,1)='-' AND substr(id,24,1)='-')
);

CREATE UNIQUE INDEX tags_active_name_key
ON tags(name_key) WHERE status='ACTIVE';

CREATE INDEX tags_active_page ON tags(status,name_key,id);
CREATE INDEX tags_updated_page ON tags(status,updated_at_ms DESC,id DESC);

CREATE TRIGGER tags_guarded_update
BEFORE UPDATE ON tags
WHEN NEW.id<>OLD.id
  OR NEW.created_by_user_id<>OLD.created_by_user_id
  OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.version<>OLD.version+1
  OR NEW.updated_at_ms<OLD.updated_at_ms
  OR OLD.status='DELETED'
  OR (OLD.status='ACTIVE' AND NEW.status NOT IN ('ACTIVE','DELETED'))
  OR (NEW.status='ACTIVE' AND NEW.deleted_at_ms IS NOT NULL)
  OR (NEW.status='DELETED' AND NEW.deleted_at_ms IS NULL)
BEGIN
  SELECT RAISE(ABORT,'invalid tag update');
END;

CREATE TRIGGER tags_no_delete
BEFORE DELETE ON tags
BEGIN
  SELECT RAISE(ABORT,'tag tombstones are immutable');
END;

CREATE TABLE game_tags (
  game_id TEXT NOT NULL REFERENCES games(id),
  tag_id TEXT NOT NULL REFERENCES tags(id),
  assigned_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(game_id,tag_id)
);

CREATE INDEX game_tags_tag ON game_tags(tag_id,game_id);

CREATE TRIGGER game_tags_validate_insert
BEFORE INSERT ON game_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE')
  OR (SELECT count(*) FROM game_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.game_id=NEW.game_id)>=20
BEGIN
  SELECT RAISE(ABORT,'invalid active game tag');
END;

CREATE TRIGGER game_tags_immutable_update
BEFORE UPDATE ON game_tags
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TABLE review_draft_tags (
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  tag_id TEXT NOT NULL REFERENCES tags(id),
  assigned_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(review_draft_id,tag_id)
);

CREATE INDEX review_draft_tags_tag ON review_draft_tags(tag_id,review_draft_id);

CREATE TRIGGER review_draft_tags_validate_insert
BEFORE INSERT ON review_draft_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM review_drafts draft
    JOIN import_items item ON item.id=draft.import_item_id
    WHERE draft.id=NEW.review_draft_id AND item.state='REVIEW_PENDING'
  )
  OR (SELECT count(*) FROM review_draft_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.review_draft_id=NEW.review_draft_id)>=20
BEGIN
  SELECT RAISE(ABORT,'invalid active review tag');
END;

CREATE TRIGGER review_draft_tags_validate_delete
BEFORE DELETE ON review_draft_tags
WHEN EXISTS(SELECT 1 FROM tags WHERE id=OLD.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM review_drafts draft
    JOIN import_items item ON item.id=draft.import_item_id
    WHERE draft.id=OLD.review_draft_id AND item.state='REVIEW_PENDING'
  )
BEGIN
  SELECT RAISE(ABORT,'review tag mapping is frozen');
END;

CREATE TRIGGER review_draft_tags_immutable_update
BEFORE UPDATE ON review_draft_tags
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

ALTER TABLE pegasus_import_collections
ADD COLUMN tag_snapshot_json TEXT NOT NULL DEFAULT '[]'
CHECK(json_valid(tag_snapshot_json) AND json_type(tag_snapshot_json)='array');

CREATE TABLE pegasus_collection_tags (
  collection_id TEXT NOT NULL REFERENCES pegasus_import_collections(id),
  tag_id TEXT NOT NULL REFERENCES tags(id),
  assigned_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(collection_id,tag_id)
);

CREATE INDEX pegasus_collection_tags_tag ON pegasus_collection_tags(tag_id,collection_id);

CREATE TRIGGER pegasus_collection_tags_validate_insert
BEFORE INSERT ON pegasus_collection_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM pegasus_import_collections collection
    JOIN pegasus_imports import ON import.id=collection.import_id
    WHERE collection.id=NEW.collection_id AND import.state='AWAITING_MAPPING'
  )
  OR (SELECT count(*) FROM pegasus_collection_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.collection_id=NEW.collection_id)>=20
BEGIN
  SELECT RAISE(ABORT,'invalid active Pegasus collection tag');
END;

CREATE TRIGGER pegasus_collection_tags_validate_delete
BEFORE DELETE ON pegasus_collection_tags
WHEN EXISTS(SELECT 1 FROM tags WHERE id=OLD.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM pegasus_import_collections collection
    JOIN pegasus_imports import ON import.id=collection.import_id
    WHERE collection.id=OLD.collection_id AND import.state='AWAITING_MAPPING'
  )
BEGIN
  SELECT RAISE(ABORT,'Pegasus collection tag mapping is frozen');
END;

CREATE TRIGGER pegasus_collection_tags_immutable_update
BEFORE UPDATE ON pegasus_collection_tags
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

DROP TRIGGER pegasus_collection_mapping_update;

CREATE TRIGGER pegasus_collection_mapping_update
BEFORE UPDATE OF mapping_action,target_platform_instance_id,target_platform_instance_version,target_platform_id,
  target_default_core_id,target_core_artifact_id,target_core_artifact_version,target_dat_version_id,tag_snapshot_json
ON pegasus_import_collections
WHEN NOT EXISTS(SELECT 1 FROM pegasus_imports import WHERE import.id=OLD.import_id AND import.state='AWAITING_MAPPING')
BEGIN
  SELECT RAISE(ABORT,'Pegasus mapping is frozen');
END;
