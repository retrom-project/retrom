CREATE TABLE dat_diff_snapshots (
  id TEXT PRIMARY KEY,
  dat_version_id TEXT NOT NULL UNIQUE REFERENCES dat_versions(id) ON DELETE CASCADE,
  base_dat_version_id TEXT REFERENCES dat_versions(id),
  state TEXT NOT NULL CHECK(state IN ('PENDING','RUNNING','READY','STALE','FAILED')),
  input_digest TEXT NOT NULL CHECK(length(input_digest) = 64),
  summary_json TEXT,
  impact_json TEXT,
  impact_digest TEXT,
  error_code TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count >= 0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  queued_at_ms INTEGER NOT NULL,
  started_at_ms INTEGER,
  completed_at_ms INTEGER,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  CHECK((state = 'READY') = (summary_json IS NOT NULL AND impact_json IS NOT NULL AND impact_digest IS NOT NULL)),
  CHECK(state <> 'RUNNING' OR started_at_ms IS NOT NULL),
  CHECK(state NOT IN ('READY','FAILED') OR completed_at_ms IS NOT NULL)
);

CREATE INDEX dat_diff_snapshots_state ON dat_diff_snapshots(state, queued_at_ms, id);

CREATE TABLE dat_diff_items (
  snapshot_id TEXT NOT NULL REFERENCES dat_diff_snapshots(id) ON DELETE CASCADE,
  section TEXT NOT NULL CHECK(section IN ('MACHINES','ROM_ENTRIES','BIOS_SETS','DEPENDENCY_TARGETS')),
  cursor_key TEXT NOT NULL,
  change_kind TEXT NOT NULL CHECK(change_kind IN ('ADDED','REMOVED','CHANGED')),
  key_json TEXT NOT NULL,
  before_json TEXT NOT NULL,
  after_json TEXT NOT NULL,
  PRIMARY KEY(snapshot_id, section, cursor_key)
);

CREATE INDEX dat_diff_items_page ON dat_diff_items(snapshot_id, section, change_kind, cursor_key);
