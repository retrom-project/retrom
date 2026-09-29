-- Shared browser runtime credentials have an independent rolling lifetime.
CREATE TABLE runtime_sessions (
  id TEXT PRIMARY KEY,
  auth_session_id TEXT NOT NULL UNIQUE REFERENCES auth_sessions(id) ON DELETE CASCADE,
  token_sha256 BLOB NOT NULL UNIQUE CHECK(length(token_sha256)=32),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  renewed_at_ms INTEGER NOT NULL CHECK(renewed_at_ms>=created_at_ms),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms=renewed_at_ms+86400000)
);
