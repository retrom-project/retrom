CREATE TABLE metadata_scrape_runs (
  id TEXT PRIMARY KEY,
  import_item_id TEXT REFERENCES import_items(id),
  game_id TEXT REFERENCES games(id),
  game_content_revision_id TEXT REFERENCES game_content_revisions(id),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  provider TEXT NOT NULL CHECK(provider IN ('HASHEOUS','NONE')),
  provider_config_version INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('RUNNING','COMPLETED','FAILED','CANCELLED')),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  completed_at_ms INTEGER,
  error_code TEXT,
  CHECK((import_item_id IS NOT NULL) != (game_id IS NOT NULL)),
  CHECK((game_id IS NULL) = (game_content_revision_id IS NULL)),
  CHECK((state = 'RUNNING') = (completed_at_ms IS NULL)),
  CHECK((state = 'FAILED') = (error_code IS NOT NULL))
);

CREATE TABLE content_hash_evidence (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL REFERENCES metadata_scrape_runs(id),
  profile TEXT NOT NULL CHECK(profile IN ('RAW_FILE_V1','SINGLE_ARCHIVE_MEMBER_V1','ARCADE_DAT_ENTRIES_V1')),
  blob_id TEXT REFERENCES blobs(id),
  archive_blob_id TEXT,
  archive_entry_ordinal INTEGER,
  crc32 TEXT,
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  query_order INTEGER NOT NULL CHECK(query_order >= 0),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(scrape_run_id, profile, query_order),
  FOREIGN KEY(archive_blob_id, archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id, ordinal),
  CHECK((blob_id IS NOT NULL) != (archive_blob_id IS NOT NULL)),
  CHECK(crc32 IS NOT NULL OR md5 IS NOT NULL OR sha1 IS NOT NULL OR sha256 IS NOT NULL)
);

CREATE TABLE metadata_provider_responses (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL CHECK(provider = 'HASHEOUS'),
  request_digest TEXT NOT NULL CHECK(length(request_digest) = 64),
  http_status INTEGER,
  outcome TEXT NOT NULL CHECK(outcome IN ('HIT','MISS','RATE_LIMITED','TIMEOUT','INVALID_RESPONSE','NETWORK_ERROR')),
  raw_response_blob_id TEXT REFERENCES blobs(id),
  fetched_at_ms INTEGER NOT NULL,
  expires_at_ms INTEGER NOT NULL
);

CREATE TABLE metadata_provider_cache (
  provider TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  current_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  expires_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  PRIMARY KEY(provider, request_digest)
);

CREATE TABLE metadata_scrape_query_attempts (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL REFERENCES metadata_scrape_runs(id),
  content_hash_evidence_id TEXT NOT NULL REFERENCES content_hash_evidence(id),
  provider_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  attempt_no INTEGER NOT NULL CHECK(attempt_no >= 1),
  source TEXT NOT NULL CHECK(source IN ('NETWORK','CACHE')),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(content_hash_evidence_id, attempt_no)
);

CREATE TABLE scrape_candidates (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL REFERENCES metadata_scrape_runs(id),
  primary_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  provider_game_id TEXT NOT NULL,
  normalized_metadata_json TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(scrape_run_id, provider_game_id)
);

CREATE TABLE scrape_candidate_hits (
  scrape_candidate_id TEXT NOT NULL REFERENCES scrape_candidates(id),
  query_attempt_id TEXT NOT NULL REFERENCES metadata_scrape_query_attempts(id),
  matched_hashes_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(scrape_candidate_id, query_attempt_id)
);

CREATE TABLE scrape_candidate_assets (
  id TEXT PRIMARY KEY,
  scrape_candidate_id TEXT NOT NULL REFERENCES scrape_candidates(id),
  provider_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  provider_asset_id TEXT NOT NULL,
  kind_hint TEXT NOT NULL CHECK(kind_hint IN ('COVER','BACKGROUND','SCREENSHOT','UNKNOWN')),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  source_path TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('PENDING','FETCHING','READY','FAILED','CANCELLED')),
  blob_id TEXT REFERENCES blobs(id),
  width_px INTEGER,
  height_px INTEGER,
  media_type TEXT,
  error_code TEXT,
  fetched_at_ms INTEGER,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(scrape_candidate_id, provider_asset_id),
  CHECK((status = 'READY') = (blob_id IS NOT NULL AND width_px IS NOT NULL AND height_px IS NOT NULL AND media_type IS NOT NULL)),
  CHECK((status IN ('FAILED','CANCELLED')) = (error_code IS NOT NULL))
);

CREATE TABLE review_drafts (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL UNIQUE REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  selected_validation_id TEXT REFERENCES import_item_core_validations(id),
  selected_candidate_id TEXT REFERENCES scrape_candidates(id),
  cover_candidate_asset_id TEXT REFERENCES scrape_candidate_assets(id),
  background_candidate_asset_id TEXT REFERENCES scrape_candidate_assets(id),
  default_dos_entry TEXT,
  metadata_json TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL
);

CREATE TABLE review_draft_screenshot_assets (
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  candidate_asset_id TEXT NOT NULL REFERENCES scrape_candidate_assets(id),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(review_draft_id, ordinal),
  UNIQUE(review_draft_id, candidate_asset_id)
);

CREATE TABLE review_events (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  event_type TEXT NOT NULL CHECK(event_type IN ('DRAFT_SAVED','TARGET_CHANGED','SCRAPE_REQUESTED','CANDIDATE_APPLIED','CANDIDATE_REMOVED','APPROVED','DISCARDED')),
  actor TEXT NOT NULL CHECK(actor = 'local'),
  before_json TEXT NOT NULL,
  after_json TEXT NOT NULL,
  diff_json TEXT NOT NULL,
  config_evidence_json TEXT NOT NULL,
  dat_evidence_json TEXT NOT NULL,
  provider_evidence_json TEXT NOT NULL,
  reason TEXT,
  created_at_ms INTEGER NOT NULL
);

CREATE INDEX review_queue ON review_drafts(updated_at_ms, import_item_id);
CREATE INDEX review_events_history ON review_events(event_type, created_at_ms, id);

CREATE TRIGGER content_hash_evidence_immutable_update BEFORE UPDATE ON content_hash_evidence BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER content_hash_evidence_immutable_delete BEFORE DELETE ON content_hash_evidence BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER provider_responses_immutable_update BEFORE UPDATE ON metadata_provider_responses BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER provider_responses_immutable_delete BEFORE DELETE ON metadata_provider_responses BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER scrape_attempts_immutable_update BEFORE UPDATE ON metadata_scrape_query_attempts BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER scrape_attempts_immutable_delete BEFORE DELETE ON metadata_scrape_query_attempts BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER scrape_candidates_immutable_update BEFORE UPDATE ON scrape_candidates BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER scrape_candidates_immutable_delete BEFORE DELETE ON scrape_candidates BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER review_events_immutable_update BEFORE UPDATE ON review_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER review_events_immutable_delete BEFORE DELETE ON review_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
