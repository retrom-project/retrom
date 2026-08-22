-- Clean pre-release baseline: identity.

CREATE TABLE profiles (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0)
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL UNIQUE REFERENCES profiles(id),
  username TEXT NOT NULL UNIQUE CHECK(
    length(username) BETWEEN 3 AND 32 AND
    username = lower(username) AND
    username GLOB '[a-z]*' AND
    username NOT GLOB '*[^a-z0-9._-]*' AND
    username NOT IN ('local','root','system','retrom')
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 320),
  role TEXT NOT NULL CHECK(role IN ('ADMIN','USER')),
  status TEXT NOT NULL CHECK(status IN ('ENABLED','DISABLED','DELETED')),
  session_version INTEGER NOT NULL DEFAULT 1 CHECK(session_version >= 1),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  last_login_at_ms INTEGER CHECK(last_login_at_ms IS NULL OR last_login_at_ms >= 0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  disabled_at_ms INTEGER CHECK(disabled_at_ms IS NULL OR disabled_at_ms >= created_at_ms),
  deleted_at_ms INTEGER CHECK(deleted_at_ms IS NULL OR deleted_at_ms >= created_at_ms),
  CHECK(
    status = 'ENABLED' AND disabled_at_ms IS NULL AND deleted_at_ms IS NULL OR
    status = 'DISABLED' AND disabled_at_ms IS NOT NULL AND deleted_at_ms IS NULL OR
    status = 'DELETED' AND deleted_at_ms IS NOT NULL
  )
);

CREATE TABLE user_credentials (
  user_id TEXT PRIMARY KEY REFERENCES users(id),
  password_hash TEXT NOT NULL CHECK(length(password_hash) BETWEEN 1 AND 512),
  password_scheme TEXT NOT NULL CHECK(password_scheme='ARGON2ID_V1'),
  password_changed_at_ms INTEGER NOT NULL CHECK(password_changed_at_ms >= 0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  CHECK(password_changed_at_ms >= created_at_ms)
);

CREATE TABLE auth_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  token_sha256 BLOB NOT NULL UNIQUE CHECK(length(token_sha256)=32),
  user_session_version INTEGER NOT NULL CHECK(user_session_version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  last_seen_at_ms INTEGER NOT NULL CHECK(last_seen_at_ms >= created_at_ms),
  idle_expires_at_ms INTEGER NOT NULL CHECK(idle_expires_at_ms >= last_seen_at_ms),
  absolute_expires_at_ms INTEGER NOT NULL CHECK(absolute_expires_at_ms >= idle_expires_at_ms),
  revoked_at_ms INTEGER,
  revoked_reason TEXT CHECK(revoked_reason IN (
    'LOGOUT','PASSWORD_CHANGED','PASSWORD_RESET','ROLE_CHANGED','USER_DISABLED',
    'USER_DELETED','OFFLINE_RECOVERY','RESTORE','EXPIRED'
  )),
  CHECK((revoked_at_ms IS NULL) = (revoked_reason IS NULL)),
  CHECK(revoked_at_ms IS NULL OR revoked_at_ms >= created_at_ms)
);

CREATE TABLE account_links (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(kind IN ('INVITATION','PASSWORD_RESET')),
  invited_role TEXT CHECK(invited_role IN ('ADMIN','USER')),
  target_user_id TEXT REFERENCES users(id),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms = created_at_ms + 3600000),
  consumed_at_ms INTEGER,
  consumed_by_user_id TEXT REFERENCES users(id),
  revoked_at_ms INTEGER,
  revoked_by_kind TEXT CHECK(revoked_by_kind IN ('USER','SYSTEM')),
  revoked_by_user_id TEXT REFERENCES users(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  CHECK(
    kind='INVITATION' AND invited_role IS NOT NULL AND target_user_id IS NULL OR
    kind='PASSWORD_RESET' AND invited_role IS NULL AND target_user_id IS NOT NULL
  ),
  CHECK((consumed_at_ms IS NULL) = (consumed_by_user_id IS NULL)),
  CHECK(
    revoked_at_ms IS NULL AND revoked_by_kind IS NULL AND revoked_by_user_id IS NULL OR
    revoked_at_ms IS NOT NULL AND revoked_by_kind='SYSTEM' AND revoked_by_user_id IS NULL OR
    revoked_at_ms IS NOT NULL AND revoked_by_kind='USER' AND revoked_by_user_id IS NOT NULL
  ),
  CHECK(consumed_at_ms IS NULL OR revoked_at_ms IS NULL)
);

CREATE TABLE instance_state (
  id INTEGER PRIMARY KEY CHECK(id=1),
  state TEXT NOT NULL CHECK(state IN ('PENDING','COMPLETED')),
  bootstrap_kind TEXT CHECK(bootstrap_kind IN ('RELEASE_SETUP','TEST_DEFAULT')),
  initial_admin_user_id TEXT REFERENCES users(id),
  test_default_password_active INTEGER NOT NULL DEFAULT 0 CHECK(test_default_password_active IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  initialized_at_ms INTEGER,
  CHECK(
    state='PENDING' AND bootstrap_kind IS NULL AND initial_admin_user_id IS NULL AND
      test_default_password_active=0 AND initialized_at_ms IS NULL OR
    state='COMPLETED' AND bootstrap_kind IS NOT NULL AND initial_admin_user_id IS NOT NULL AND
      initialized_at_ms IS NOT NULL AND initialized_at_ms >= created_at_ms
  ),
  CHECK(test_default_password_active=0 OR bootstrap_kind='TEST_DEFAULT')
);

CREATE TABLE auth_rate_limits (
  scope TEXT NOT NULL CHECK(scope IN ('LOGIN_ACCOUNT','LOGIN_IP','SETUP_IP','LINK_IP')),
  subject_hash BLOB NOT NULL CHECK(length(subject_hash)=32),
  window_started_at_ms INTEGER NOT NULL CHECK(window_started_at_ms >= 0),
  failure_count INTEGER NOT NULL CHECK(failure_count >= 0),
  blocked_until_ms INTEGER CHECK(blocked_until_ms IS NULL OR blocked_until_ms >= window_started_at_ms),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= window_started_at_ms),
  PRIMARY KEY(scope,subject_hash)
);

INSERT INTO instance_state(
  id,state,bootstrap_kind,initial_admin_user_id,test_default_password_active,
  version,created_at_ms,updated_at_ms,initialized_at_ms
) VALUES(1,'PENDING',NULL,NULL,0,1,0,0,NULL);
