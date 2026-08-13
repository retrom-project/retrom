CREATE TABLE netplay_rooms (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  host_profile_id TEXT NOT NULL REFERENCES profiles(id),
  state TEXT NOT NULL CHECK(state IN ('DRAFT','WAITING','STARTING','RUNNING','ENDED','EXPIRED')),
  selected_game_id TEXT REFERENCES games(id),
  selected_game_variant_revision_id TEXT REFERENCES game_variant_revisions(id),
  netplay_profile_id TEXT,
  profile_digest TEXT CHECK(profile_digest IS NULL OR profile_digest GLOB '[0-9a-f]*' AND length(profile_digest)=64),
  max_players INTEGER CHECK(max_players IS NULL OR max_players BETWEEN 2 AND 4),
  current_session_id TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  ended_at_ms INTEGER,
  end_reason TEXT CHECK(end_reason IS NULL OR end_reason IN (
    'NORMAL','USER_EXIT','HOST_CLOSED','HOST_LOST','PEER_TIMEOUT','AUTH_REVOKED','START_TIMEOUT',
    'PREPARE_FAILED','PROFILE_REVOKED','SERVER_RESTARTED','RESTORE','HARD_EXPIRED',
    'ROLLBACK_WINDOW_EXCEEDED','STATE_RING_CAPACITY_EXCEEDED','STATE_TRANSFER_TIMEOUT',
    'STATE_INVALID','NETPLAY_UNSTABLE','PEER_TOO_SLOW','PROTOCOL_VIOLATION','INTERNAL_ERROR'
  )),
  CHECK(
    selected_game_id IS NULL AND selected_game_variant_revision_id IS NULL AND netplay_profile_id IS NULL
      AND profile_digest IS NULL AND max_players IS NULL OR
    selected_game_id IS NOT NULL AND selected_game_variant_revision_id IS NOT NULL AND netplay_profile_id IS NOT NULL
      AND profile_digest IS NOT NULL AND max_players IS NOT NULL
  ),
  CHECK(state!='DRAFT' OR selected_game_id IS NULL),
  CHECK(state NOT IN ('WAITING','STARTING','RUNNING') OR selected_game_id IS NOT NULL),
  CHECK((state IN ('STARTING','RUNNING'))=(current_session_id IS NOT NULL)),
  CHECK((state IN ('ENDED','EXPIRED'))=(ended_at_ms IS NOT NULL)),
  CHECK((ended_at_ms IS NULL)=(end_reason IS NULL)),
  CHECK(ended_at_ms IS NULL OR ended_at_ms>=created_at_ms)
);
CREATE UNIQUE INDEX netplay_rooms_one_active_host
ON netplay_rooms(host_profile_id) WHERE state IN ('DRAFT','WAITING','STARTING','RUNNING');
CREATE INDEX netplay_rooms_expiry ON netplay_rooms(state,expires_at_ms,id);

CREATE TABLE netplay_room_members (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  role TEXT NOT NULL CHECK(role IN ('HOST','GUEST')),
  player_no INTEGER NOT NULL CHECK(player_no BETWEEN 1 AND 4),
  ready INTEGER NOT NULL DEFAULT 0 CHECK(ready IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  joined_at_ms INTEGER NOT NULL CHECK(joined_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=joined_at_ms),
  left_at_ms INTEGER,
  leave_reason TEXT CHECK(leave_reason IS NULL OR leave_reason IN ('USER_LEFT','HOST_KICKED','SESSION_ENDED','ROOM_ENDED','AUTH_REVOKED')),
  UNIQUE(room_id,profile_id),
  CHECK((role='HOST')=(player_no=1)),
  CHECK((left_at_ms IS NULL)=(leave_reason IS NULL)),
  CHECK(left_at_ms IS NULL OR left_at_ms>=joined_at_ms),
  CHECK(left_at_ms IS NULL OR ready=0)
);
CREATE UNIQUE INDEX netplay_room_members_active_seat
ON netplay_room_members(room_id,player_no) WHERE left_at_ms IS NULL;
CREATE INDEX netplay_room_members_profile ON netplay_room_members(profile_id,left_at_ms,room_id);

CREATE TABLE netplay_sessions (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  session_no INTEGER NOT NULL CHECK(session_no>=1),
  state TEXT NOT NULL CHECK(state IN (
    'PREPARING','LOADING','SYNCHRONIZING','RUNNING','PAUSED_RECONNECT','RESYNCHRONIZING','FINISHED','FAILED'
  )),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  netplay_profile_id TEXT NOT NULL,
  profile_json TEXT NOT NULL CHECK(json_valid(profile_json) AND json_type(profile_json)='object'),
  profile_digest TEXT NOT NULL CHECK(profile_digest GLOB '[0-9a-f]*' AND length(profile_digest)=64),
  player_count INTEGER NOT NULL CHECK(player_count BETWEEN 2 AND 4),
  occupied_seat_mask INTEGER NOT NULL CHECK(occupied_seat_mask BETWEEN 3 AND 15 AND (occupied_seat_mask & 1)=1),
  authority_player_no INTEGER NOT NULL DEFAULT 1 CHECK(authority_player_no=1),
  resync_count INTEGER NOT NULL DEFAULT 0 CHECK(resync_count>=0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  started_at_ms INTEGER,
  finished_at_ms INTEGER,
  end_reason TEXT CHECK(end_reason IS NULL OR end_reason IN (
    'NORMAL','USER_EXIT','HOST_CLOSED','HOST_LOST','PEER_TIMEOUT','AUTH_REVOKED','START_TIMEOUT',
    'PREPARE_FAILED','PROFILE_REVOKED','SERVER_RESTARTED','RESTORE','HARD_EXPIRED',
    'ROLLBACK_WINDOW_EXCEEDED','STATE_RING_CAPACITY_EXCEEDED','STATE_TRANSFER_TIMEOUT',
    'STATE_INVALID','NETPLAY_UNSTABLE','PEER_TOO_SLOW','PROTOCOL_VIOLATION','INTERNAL_ERROR'
  )),
  UNIQUE(room_id,session_no),
  CHECK((state IN ('FINISHED','FAILED'))=(finished_at_ms IS NOT NULL)),
  CHECK((finished_at_ms IS NULL)=(end_reason IS NULL)),
  CHECK(started_at_ms IS NULL OR started_at_ms>=created_at_ms),
  CHECK(finished_at_ms IS NULL OR finished_at_ms>=created_at_ms)
);
CREATE UNIQUE INDEX netplay_sessions_one_active_room
ON netplay_sessions(room_id) WHERE state NOT IN ('FINISHED','FAILED');
CREATE INDEX netplay_sessions_state ON netplay_sessions(state,updated_at_ms,id);

CREATE TABLE netplay_session_participants (
  netplay_session_id TEXT NOT NULL REFERENCES netplay_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  room_member_id TEXT NOT NULL REFERENCES netplay_room_members(id),
  player_no INTEGER NOT NULL CHECK(player_no BETWEEN 1 AND 4),
  launch_session_id TEXT UNIQUE REFERENCES launch_sessions(id),
  credential_sha256 BLOB CHECK(credential_sha256 IS NULL OR length(credential_sha256)=32),
  state TEXT NOT NULL CHECK(state IN (
    'LOCKED','LAUNCH_READY','RUNTIME_READY','SYNCHRONIZED','CONNECTED','DISCONNECTED','LEFT'
  )),
  credential_generation INTEGER NOT NULL DEFAULT 0 CHECK(credential_generation>=0),
  disconnected_at_ms INTEGER,
  lease_expires_at_ms INTEGER,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(netplay_session_id,profile_id),
  UNIQUE(netplay_session_id,player_no),
  UNIQUE(netplay_session_id,room_member_id),
  CHECK(
    state='LOCKED' AND launch_session_id IS NULL AND credential_sha256 IS NULL AND credential_generation=0 OR
    state='LEFT' AND (
      launch_session_id IS NULL AND credential_sha256 IS NULL AND credential_generation=0 OR
      launch_session_id IS NOT NULL AND credential_sha256 IS NOT NULL AND credential_generation>=1
    ) OR
    state NOT IN ('LOCKED','LEFT') AND launch_session_id IS NOT NULL AND credential_sha256 IS NOT NULL AND credential_generation>=1
  ),
  CHECK(
    state='DISCONNECTED' AND disconnected_at_ms IS NOT NULL AND lease_expires_at_ms IS NOT NULL
      AND lease_expires_at_ms>=disconnected_at_ms OR
    state!='DISCONNECTED' AND disconnected_at_ms IS NULL AND lease_expires_at_ms IS NULL
  )
);
CREATE INDEX netplay_session_participants_profile
ON netplay_session_participants(profile_id,state,netplay_session_id);

CREATE TABLE netplay_events (
  id INTEGER PRIMARY KEY,
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  netplay_session_id TEXT REFERENCES netplay_sessions(id),
  profile_id TEXT REFERENCES profiles(id),
  player_no INTEGER CHECK(player_no IS NULL OR player_no BETWEEN 1 AND 4),
  event_type TEXT NOT NULL CHECK(event_type IN (
    'ROOM_CREATED','GAME_SELECTED','GAME_CLEARED','MEMBER_JOINED','SEAT_CHANGED','READY_CHANGED',
    'MEMBER_LEFT','MEMBER_KICKED','SESSION_CREATED','SESSION_STATE_CHANGED',
    'PARTICIPANT_STATE_CHANGED','PAUSED','RESUMED','RESYNCED','ROOM_ENDED','ROOM_EXPIRED'
  )),
  result_code TEXT CHECK(result_code IS NULL OR length(CAST(result_code AS BLOB)) BETWEEN 1 AND 64),
  data_json TEXT NOT NULL CHECK(json_valid(data_json) AND json_type(data_json)='object'),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)
);
CREATE INDEX netplay_events_room ON netplay_events(room_id,id);
CREATE INDEX netplay_events_session ON netplay_events(netplay_session_id,id);

ALTER TABLE launch_sessions ADD COLUMN netplay_session_id TEXT REFERENCES netplay_sessions(id);
ALTER TABLE launch_sessions ADD COLUMN netplay_player_no INTEGER CHECK(netplay_player_no IS NULL OR netplay_player_no BETWEEN 1 AND 4);
ALTER TABLE launch_sessions ADD COLUMN save_access TEXT NOT NULL DEFAULT 'NORMAL'
  CHECK(save_access IN ('NORMAL','NETPLAY_DISABLED'));

CREATE UNIQUE INDEX launch_sessions_one_netplay_participant
ON launch_sessions(netplay_session_id,profile_id) WHERE netplay_session_id IS NOT NULL;

CREATE TRIGGER netplay_rooms_host_immutable
BEFORE UPDATE OF host_profile_id,created_at_ms ON netplay_rooms
BEGIN SELECT RAISE(ABORT,'immutable netplay room identity'); END;
CREATE TRIGGER netplay_rooms_snapshot_immutable
BEFORE UPDATE OF selected_game_id,selected_game_variant_revision_id,netplay_profile_id,profile_digest,max_players
ON netplay_rooms WHEN OLD.state IN ('STARTING','RUNNING')
BEGIN SELECT RAISE(ABORT,'locked netplay room snapshot'); END;
CREATE TRIGGER netplay_rooms_current_session_immutable
BEFORE UPDATE OF current_session_id ON netplay_rooms
WHEN OLD.current_session_id IS NOT NULL AND NEW.current_session_id IS NOT OLD.current_session_id
  AND NEW.state IN ('STARTING','RUNNING')
BEGIN SELECT RAISE(ABORT,'locked netplay room session'); END;

CREATE TRIGGER netplay_room_members_validate_insert
BEFORE INSERT ON netplay_room_members
WHEN NEW.role='HOST' AND NOT EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.host_profile_id=NEW.profile_id
) OR NEW.role='GUEST' AND EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.host_profile_id=NEW.profile_id
) OR NEW.ready=1 AND NOT EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.state='WAITING'
)
BEGIN SELECT RAISE(ABORT,'invalid netplay room member'); END;
CREATE TRIGGER netplay_room_members_validate_update
BEFORE UPDATE ON netplay_room_members
WHEN NEW.room_id!=OLD.room_id OR NEW.profile_id!=OLD.profile_id OR NEW.role!=OLD.role OR
  NEW.role='HOST' AND NEW.player_no!=1 OR NEW.ready=1 AND (
    NEW.left_at_ms IS NOT NULL OR NOT EXISTS(
      SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.state='WAITING'
    )
  )
BEGIN SELECT RAISE(ABORT,'invalid netplay room member update'); END;

CREATE TRIGGER netplay_sessions_snapshot_immutable
BEFORE UPDATE OF room_id,session_no,game_id,game_variant_revision_id,core_artifact_id,netplay_profile_id,
  profile_json,profile_digest,player_count,occupied_seat_mask,authority_player_no,created_at_ms
ON netplay_sessions
BEGIN SELECT RAISE(ABORT,'immutable netplay session snapshot'); END;
CREATE TRIGGER netplay_sessions_validate_insert
BEFORE INSERT ON netplay_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM netplay_rooms room
  WHERE room.id=NEW.room_id AND room.selected_game_id=NEW.game_id
    AND room.selected_game_variant_revision_id=NEW.game_variant_revision_id
    AND room.netplay_profile_id=NEW.netplay_profile_id AND room.profile_digest=NEW.profile_digest
)
BEGIN SELECT RAISE(ABORT,'invalid netplay session snapshot'); END;

CREATE TRIGGER netplay_session_participants_immutable_identity
BEFORE UPDATE OF netplay_session_id,profile_id,room_member_id,player_no,launch_session_id,credential_sha256,credential_generation
ON netplay_session_participants WHEN OLD.launch_session_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'immutable netplay participant identity'); END;
CREATE TRIGGER netplay_session_participants_validate_insert
BEFORE INSERT ON netplay_session_participants
WHEN NOT EXISTS(
  SELECT 1 FROM netplay_sessions session
  JOIN netplay_room_members member ON member.room_id=session.room_id
  WHERE session.id=NEW.netplay_session_id AND member.id=NEW.room_member_id
    AND member.profile_id=NEW.profile_id AND member.player_no=NEW.player_no AND member.left_at_ms IS NULL
)
BEGIN SELECT RAISE(ABORT,'invalid netplay participant snapshot'); END;

CREATE TRIGGER netplay_events_immutable_update
BEFORE UPDATE ON netplay_events BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER netplay_events_immutable_delete
BEFORE DELETE ON netplay_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER launch_sessions_netplay_validate_insert
BEFORE INSERT ON launch_sessions
WHEN NOT (
  NEW.netplay_session_id IS NULL AND NEW.netplay_player_no IS NULL AND NEW.save_access='NORMAL' OR
  NEW.netplay_session_id IS NOT NULL AND NEW.netplay_player_no IS NOT NULL AND NEW.save_access='NETPLAY_DISABLED'
) OR NEW.netplay_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_session_participants participant
  JOIN netplay_sessions session ON session.id=participant.netplay_session_id
  WHERE participant.netplay_session_id=NEW.netplay_session_id AND participant.profile_id=NEW.profile_id
    AND participant.player_no=NEW.netplay_player_no AND session.game_id=NEW.game_id
    AND session.game_variant_revision_id=NEW.game_variant_revision_id
    AND session.core_artifact_id=NEW.core_artifact_id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay launch'); END;
CREATE TRIGGER launch_sessions_netplay_immutable
BEFORE UPDATE OF netplay_session_id,netplay_player_no,save_access ON launch_sessions
BEGIN SELECT RAISE(ABORT,'immutable netplay launch binding'); END;

CREATE TRIGGER netplay_rooms_require_host_after_update
AFTER UPDATE ON netplay_rooms WHEN NEW.state IN ('DRAFT','WAITING','STARTING','RUNNING') AND NOT EXISTS(
  SELECT 1 FROM netplay_room_members member
  WHERE member.room_id=NEW.id AND member.profile_id=NEW.host_profile_id AND member.role='HOST'
    AND member.player_no=1 AND member.left_at_ms IS NULL
)
BEGIN SELECT RAISE(ABORT,'active netplay room requires host'); END;

CREATE TRIGGER netplay_rooms_current_session_fk_insert
BEFORE INSERT ON netplay_rooms WHEN NEW.current_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_sessions session WHERE session.id=NEW.current_session_id AND session.room_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay current session'); END;
CREATE TRIGGER netplay_rooms_current_session_fk_update
BEFORE UPDATE OF current_session_id ON netplay_rooms WHEN NEW.current_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_sessions session WHERE session.id=NEW.current_session_id AND session.room_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay current session'); END;
