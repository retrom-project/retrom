-- One durable disposition per import batch; payload deletion stays in PAYLOAD_RELEASE/GC.
CREATE TABLE import_batch_discards (
  kind TEXT NOT NULL CHECK(kind IN ('IMPORT','PEGASUS','EMULATIONSTATION')),
  import_id TEXT NOT NULL,
  requested_by_user_id TEXT NOT NULL REFERENCES users(id),
  state TEXT NOT NULL CHECK(state IN ('REQUESTED','COMPLETED','FAILED')),
  error_code TEXT,
  requested_at_ms INTEGER NOT NULL CHECK(requested_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=requested_at_ms),
  completed_at_ms INTEGER,
  PRIMARY KEY(kind,import_id),
  CHECK((state='COMPLETED')=(completed_at_ms IS NOT NULL)),
  CHECK((state='FAILED')=(error_code IS NOT NULL))
);

CREATE TABLE server_import_upload_owners (
 upload_session_id TEXT PRIMARY KEY REFERENCES upload_sessions(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('PEGASUS','EMULATIONSTATION')),
 source_item_id TEXT NOT NULL,
 UNIQUE(kind,source_item_id)
);
