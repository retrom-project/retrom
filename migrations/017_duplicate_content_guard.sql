ALTER TABLE import_jobs
ADD COLUMN already_imported_item_count INTEGER NOT NULL DEFAULT 0
CHECK(already_imported_item_count BETWEEN 0 AND discarded_item_count);

ALTER TABLE import_jobs
ADD COLUMN already_imported_file_count INTEGER NOT NULL DEFAULT 0
CHECK(already_imported_file_count >= 0);

CREATE TABLE content_identity_claims (
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(platform_id, content_identity_digest)
);

CREATE TABLE import_item_duplicate_matches (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  existing_game_id TEXT NOT NULL REFERENCES games(id),
  existing_game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  detected_stage TEXT NOT NULL CHECK(detected_stage = 'IDENTIFICATION'),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_item_id, existing_game_id)
);

CREATE INDEX fk_import_item_duplicate_matches_game
ON import_item_duplicate_matches(existing_game_id);

CREATE INDEX fk_import_item_duplicate_matches_content_revision
ON import_item_duplicate_matches(existing_game_content_revision_id);

CREATE TRIGGER content_identity_claims_immutable_update
BEFORE UPDATE ON content_identity_claims
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER content_identity_claims_immutable_delete
BEFORE DELETE ON content_identity_claims
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER import_item_duplicate_matches_immutable_update
BEFORE UPDATE ON import_item_duplicate_matches
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER import_item_duplicate_matches_immutable_delete
BEFORE DELETE ON import_item_duplicate_matches
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;
