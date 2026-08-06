CREATE TABLE profiles (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0)
);

CREATE TABLE platforms (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE cores (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  requires_threads INTEGER NOT NULL CHECK(requires_threads IN (0,1)),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE core_artifacts (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  emulatorjs_version TEXT NOT NULL,
  bundle_version TEXT NOT NULL,
  flavor TEXT NOT NULL CHECK(flavor IN ('WASM','THREAD_WASM','OVERRIDE')),
  relative_path TEXT NOT NULL CHECK(relative_path NOT LIKE '/%' AND relative_path NOT LIKE '%..%'),
  size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64 AND sha256 = lower(sha256)),
  source_commit TEXT CHECK(source_commit IS NULL OR length(source_commit) = 40),
  provenance_json TEXT NOT NULL,
  compatibility_config_json TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  UNIQUE(core_id, emulatorjs_version, sha256),
  UNIQUE(emulatorjs_version, relative_path),
  UNIQUE(id, core_id)
);
CREATE UNIQUE INDEX core_artifacts_one_enabled_per_core ON core_artifacts(core_id) WHERE enabled = 1;

CREATE TABLE platform_cores (
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  PRIMARY KEY(platform_id, core_id)
);

CREATE TABLE platform_instances (
  id TEXT PRIMARY KEY,
  platform_id TEXT NOT NULL,
  default_core_id TEXT NOT NULL,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
  slug TEXT NOT NULL CHECK(slug = lower(slug) AND slug NOT LIKE '-%' AND slug NOT LIKE '%-' AND slug NOT LIKE '%--%'),
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  deleted_at_ms INTEGER CHECK(deleted_at_ms IS NULL OR deleted_at_ms >= created_at_ms),
  UNIQUE(platform_id, slug),
  UNIQUE(id, platform_id),
  FOREIGN KEY(platform_id, default_core_id) REFERENCES platform_cores(platform_id, core_id)
);

CREATE TRIGGER platform_instances_enabled_default_insert
BEFORE INSERT ON platform_instances
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM platform_cores
    WHERE platform_id = NEW.platform_id AND core_id = NEW.default_core_id AND enabled = 1
  ) THEN RAISE(ABORT, 'platform default core is not enabled') END;
END;

CREATE TRIGGER platform_instances_enabled_default_update
BEFORE UPDATE OF default_core_id ON platform_instances
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM platform_cores
    WHERE platform_id = NEW.platform_id AND core_id = NEW.default_core_id AND enabled = 1
  ) THEN RAISE(ABORT, 'platform default core is not enabled') END;
END;

INSERT INTO profiles(id, display_name, created_at_ms) VALUES ('local', '本地玩家', 0);
INSERT INTO platforms(id, name, sort_order, enabled, created_at_ms, updated_at_ms) VALUES
  ('nes','NES / Famicom',10,1,0,0),
  ('fds','Famicom Disk System',20,1,0,0),
  ('snes','SNES',30,1,0,0),
  ('gbc','Game Boy / Color',40,1,0,0),
  ('gba','Game Boy Advance',50,1,0,0),
  ('arcade','Arcade',60,1,0,0),
  ('dos','MS-DOS',70,1,0,0);
INSERT INTO cores(id, name, requires_threads, enabled, created_at_ms, updated_at_ms) VALUES
  ('fceumm','FCEUmm',0,1,0,0),
  ('snes9x','Snes9x',0,1,0,0),
  ('gambatte','Gambatte',0,1,0,0),
  ('mgba','mGBA',0,1,0,0),
  ('fbneo','FinalBurn Neo',0,1,0,0),
  ('mame2003','MAME 2003',0,1,0,0),
  ('mame2003_plus','MAME 2003 Plus',0,1,0,0),
  ('dosbox_pure','DOSBox Pure',1,1,0,0);
INSERT INTO platform_cores(platform_id, core_id, enabled) VALUES
  ('nes','fceumm',1),('fds','fceumm',1),('snes','snes9x',1),
  ('gbc','gambatte',1),('gbc','mgba',1),('gba','mgba',1),
  ('arcade','fbneo',1),('arcade','mame2003_plus',1),('arcade','mame2003',1),
  ('dos','dosbox_pure',1);
INSERT INTO platform_instances(id, platform_id, default_core_id, name, slug, description, sort_order, enabled, version, created_at_ms, updated_at_ms) VALUES
  ('01980000-0000-7000-8000-000000000001','nes','fceumm','NES 游戏','nes-games','',10,1,1,0,0),
  ('01980000-0000-7000-8000-000000000002','fds','fceumm','FDS 游戏','fds-games','',20,1,1,0,0),
  ('01980000-0000-7000-8000-000000000003','snes','snes9x','SNES 游戏','snes-games','',30,1,1,0,0),
  ('01980000-0000-7000-8000-000000000004','gbc','gambatte','Game Boy 游戏','gbc-games','',40,1,1,0,0),
  ('01980000-0000-7000-8000-000000000005','gba','mgba','GBA 游戏','gba-games','',50,1,1,0,0),
  ('01980000-0000-7000-8000-000000000006','arcade','fbneo','FBNeo 游戏','fbneo-games','',60,1,1,0,0),
  ('01980000-0000-7000-8000-000000000007','arcade','mame2003_plus','MAME 2003 Plus 游戏','mame2003-plus-games','',70,1,1,0,0),
  ('01980000-0000-7000-8000-000000000008','arcade','mame2003','MAME 2003 游戏','mame2003-games','',80,1,1,0,0),
  ('01980000-0000-7000-8000-000000000009','dos','dosbox_pure','DOS 经典游戏','dos-games','',90,1,1,0,0);
