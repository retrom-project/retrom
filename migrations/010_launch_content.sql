CREATE TABLE launch_content_files (
  launch_session_id TEXT PRIMARY KEY REFERENCES launch_sessions(id),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 512),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  format_version TEXT NOT NULL CHECK(format_version IN ('SOURCE_V1','RETROM_DOS_DIRECT_ZIP_V1')),
  created_at_ms INTEGER NOT NULL
);

CREATE TRIGGER launch_content_files_immutable_update
BEFORE UPDATE ON launch_content_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_content_files_immutable_delete
BEFORE DELETE ON launch_content_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
