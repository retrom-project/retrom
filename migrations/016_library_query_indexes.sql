-- Play sessions remain the authoritative session ledger. This per-game read
-- model is maintained in the same transaction as every accepted snapshot.
CREATE TABLE profile_game_activity (
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  last_played_at_ms INTEGER NOT NULL CHECK(last_played_at_ms>=0),
  active_duration_ms INTEGER NOT NULL CHECK(active_duration_ms>=0),
  session_count INTEGER NOT NULL CHECK(session_count>0),
  PRIMARY KEY(profile_id,game_id)
);

INSERT INTO profile_game_activity(profile_id,game_id,last_played_at_ms,active_duration_ms,session_count)
SELECT profile_id,game_id,max(started_at_ms),sum(active_duration_ms),count(*)
FROM play_sessions GROUP BY profile_id,game_id;

CREATE INDEX profile_game_activity_recent
ON profile_game_activity(profile_id,last_played_at_ms DESC,game_id DESC);
CREATE INDEX profile_game_activity_duration
ON profile_game_activity(profile_id,active_duration_ms DESC,last_played_at_ms DESC,game_id DESC);
CREATE INDEX profile_game_activity_sessions
ON profile_game_activity(profile_id,session_count DESC,last_played_at_ms DESC,game_id DESC);
CREATE INDEX play_sessions_profile_started
ON play_sessions(profile_id,started_at_ms DESC,id DESC);
CREATE INDEX play_sessions_profile_game
ON play_sessions(profile_id,game_id,started_at_ms DESC);
CREATE INDEX games_updated ON games(updated_at_ms DESC,title ASC,id ASC);
CREATE INDEX games_added ON games(created_at_ms DESC,title ASC,id ASC);
CREATE INDEX games_title ON games(title ASC,id ASC);
CREATE INDEX game_assets_primary ON game_assets(game_id,kind,ordinal,id);

CREATE INDEX games_latest ON games(status,created_at_ms DESC,id DESC);

CREATE INDEX profile_game_activity_game ON profile_game_activity(game_id,profile_id);
CREATE INDEX play_sessions_game_profile ON play_sessions(game_id,profile_id,started_at_ms DESC);
