CREATE TABLE favorite_games (
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(profile_id,game_id),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-'),
  CHECK(length(game_id)=36 AND lower(game_id)=game_id
    AND game_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(game_id,9,1)='-' AND substr(game_id,14,1)='-'
    AND substr(game_id,19,1)='-' AND substr(game_id,24,1)='-')
);

CREATE TABLE favorite_folders (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 40 AND length(CAST(name AS BLOB))<=160),
  name_key TEXT NOT NULL CHECK(length(name_key)>=1 AND length(CAST(name_key AS BLOB))<=160),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(profile_id,id),
  UNIQUE(profile_id,name_key),
  CHECK(length(id)=36 AND lower(id)=id
    AND id NOT GLOB '*[^0-9a-f-]*'
    AND substr(id,9,1)='-' AND substr(id,14,1)='-'
    AND substr(id,19,1)='-' AND substr(id,24,1)='-'),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-')
);

CREATE TABLE favorite_folder_games (
  profile_id TEXT NOT NULL,
  folder_id TEXT NOT NULL,
  game_id TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(profile_id,folder_id,game_id),
  FOREIGN KEY(profile_id,folder_id) REFERENCES favorite_folders(profile_id,id),
  FOREIGN KEY(profile_id,game_id) REFERENCES favorite_games(profile_id,game_id),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-'),
  CHECK(length(folder_id)=36 AND lower(folder_id)=folder_id
    AND folder_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(folder_id,9,1)='-' AND substr(folder_id,14,1)='-'
    AND substr(folder_id,19,1)='-' AND substr(folder_id,24,1)='-'),
  CHECK(length(game_id)=36 AND lower(game_id)=game_id
    AND game_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(game_id,9,1)='-' AND substr(game_id,14,1)='-'
    AND substr(game_id,19,1)='-' AND substr(game_id,24,1)='-')
);

CREATE INDEX favorite_games_profile_created
ON favorite_games(profile_id,created_at_ms DESC,game_id DESC);

CREATE INDEX favorite_games_game
ON favorite_games(game_id,profile_id);

CREATE INDEX favorite_folders_profile_created
ON favorite_folders(profile_id,created_at_ms,id);

CREATE INDEX favorite_folder_games_folder
ON favorite_folder_games(profile_id,folder_id,created_at_ms,game_id);

CREATE INDEX favorite_folder_games_game
ON favorite_folder_games(profile_id,game_id,folder_id);

CREATE TRIGGER favorite_games_immutable_update
BEFORE UPDATE ON favorite_games
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER favorite_folder_games_immutable_update
BEFORE UPDATE ON favorite_folder_games
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER favorite_folders_guarded_update
BEFORE UPDATE ON favorite_folders
WHEN NEW.id<>OLD.id
  OR NEW.profile_id<>OLD.profile_id
  OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.version<>OLD.version+1
  OR NEW.updated_at_ms<OLD.updated_at_ms
  OR (NEW.name=OLD.name AND NEW.name_key=OLD.name_key)
BEGIN
  SELECT RAISE(ABORT,'invalid favorite folder update');
END;
