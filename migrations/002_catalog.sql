-- Clean pre-release baseline: catalog.

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
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE platform_cores (
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  PRIMARY KEY(platform_id, core_id)
);

CREATE TABLE runtime_providers (
  provider_id TEXT PRIMARY KEY CHECK(
    length(provider_id) BETWEEN 1 AND 64 AND provider_id=lower(provider_id)
    AND provider_id NOT GLOB '*[^a-z0-9-]*'
  ),
  provider_version TEXT NOT NULL CHECK(length(provider_version) BETWEEN 5 AND 128),
  provider_api_version INTEGER NOT NULL CHECK(provider_api_version=1),
  bundle_sha256 TEXT NOT NULL UNIQUE CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256=lower(manifest_sha256)),
  module_sha256 TEXT NOT NULL CHECK(length(module_sha256)=64 AND module_sha256=lower(module_sha256)),
  source TEXT NOT NULL CHECK(source IN ('candidate','production')),
  release_repository TEXT,
  release_tag TEXT,
  release_commit TEXT,
  activated_at_ms INTEGER NOT NULL CHECK(activated_at_ms>=0),
  UNIQUE(provider_id,bundle_sha256),
  CHECK(
    source='candidate' AND release_repository IS NULL AND release_tag IS NULL AND release_commit IS NULL
    OR source='production' AND release_repository IS NOT NULL AND release_tag IS NOT NULL
      AND length(release_commit)=40 AND release_commit=lower(release_commit)
  )
);

CREATE TABLE runtime_targets (
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id) ON DELETE CASCADE,
  target_id TEXT NOT NULL CHECK(
    length(target_id) BETWEEN 1 AND 64 AND target_id=lower(target_id)
    AND target_id NOT GLOB '*[^a-z0-9-]*'
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 120),
  game_compatibility_line TEXT NOT NULL CHECK(length(game_compatibility_line) BETWEEN 1 AND 64),
  netplay_compatibility_line TEXT CHECK(length(netplay_compatibility_line) BETWEEN 1 AND 64),
  options_kind TEXT NOT NULL CHECK(options_kind IN (
    'NONE_V1','EMULATORJS_V1','RPGMAKER_V1','ONS_PROJECT_V1','KIRIKIRI_PROJECT_V1'
  )),
  capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
  checkpoint_json TEXT CHECK(checkpoint_json IS NULL OR json_valid(checkpoint_json)),
  manifest_fragment_json TEXT NOT NULL CHECK(json_valid(manifest_fragment_json)),
  target_contract_sha256 TEXT NOT NULL CHECK(
    length(target_contract_sha256)=64 AND target_contract_sha256=lower(target_contract_sha256)
  ),
  PRIMARY KEY(provider_id,target_id),
  UNIQUE(provider_id,target_id,target_contract_sha256),
  UNIQUE(provider_id,target_id,target_contract_sha256,game_compatibility_line),
  UNIQUE(provider_id,target_id,target_contract_sha256,netplay_compatibility_line)
);

CREATE TABLE runtime_catalog_state (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  catalog_version INTEGER NOT NULL CHECK(catalog_version>=1),
  catalog_sha256 TEXT NOT NULL CHECK(length(catalog_sha256)=64 AND catalog_sha256=lower(catalog_sha256)),
  activated_at_ms INTEGER NOT NULL CHECK(activated_at_ms>=0)
);

CREATE TABLE runtime_target_bindings (
  binding_id TEXT PRIMARY KEY CHECK(
    length(binding_id) BETWEEN 1 AND 128 AND binding_id=lower(binding_id)
    AND binding_id NOT GLOB '*[^a-z0-9-]*'
  ),
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  detector_profile TEXT NOT NULL CHECK(length(detector_profile) BETWEEN 2 AND 64),
  delivery_profile TEXT NOT NULL CHECK(length(delivery_profile) BETWEEN 2 AND 64),
  launch_policy TEXT NOT NULL CHECK(launch_policy IN ('SUPPORTED','EXPERIMENTAL','DISABLED')),
  review_policy TEXT NOT NULL CHECK(review_policy IN ('NONE','RPG_RUNTIME_VALIDATION_V1')),
  UNIQUE(provider_id,target_id),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) ON DELETE CASCADE
);

CREATE TABLE runtime_binding_platforms (
  binding_id TEXT NOT NULL REFERENCES runtime_target_bindings(binding_id) ON DELETE CASCADE,
  platform_id TEXT NOT NULL,
  core_id TEXT NOT NULL,
  PRIMARY KEY(binding_id,platform_id),
  FOREIGN KEY(platform_id,core_id) REFERENCES platform_cores(platform_id,core_id)
);

CREATE TABLE runtime_binding_content_kinds (
  binding_id TEXT NOT NULL REFERENCES runtime_target_bindings(binding_id) ON DELETE CASCADE,
  content_kind TEXT NOT NULL CHECK(content_kind IN (
    'SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1','RPG_MAKER_PROJECT_V1','ONS_PROJECT_V1',
    'KIRIKIRI_PROJECT_V1','BUTTERSCOTCH_PROJECT_V1','TYRANOSCRIPT_PROJECT_V1'
  )),
  PRIMARY KEY(binding_id,content_kind)
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
  deleted_at_ms INTEGER CHECK(deleted_at_ms IS NULL OR deleted_at_ms >= created_at_ms), catalog_template_key TEXT
CHECK (
  catalog_template_key IS NULL OR (
    length(catalog_template_key) BETWEEN 3 AND 160
    AND catalog_template_key = lower(catalog_template_key)
    AND catalog_template_key NOT GLOB '*[^a-z0-9_/-]*'
    AND catalog_template_key GLOB '*/*'
    AND catalog_template_key NOT GLOB '*/*/*'
  )
),
  UNIQUE(platform_id, slug),
  UNIQUE(id, platform_id),
  FOREIGN KEY(platform_id, default_core_id) REFERENCES platform_cores(platform_id, core_id)
);

-- Stable reference catalog; instance data is intentionally empty.
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('3do','3DO',200,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('arcade','Arcade',60,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('atari2600','Atari 2600',90,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('atari5200','Atari 5200',100,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('atari7800','Atari 7800',110,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('butterscotch','GameMaker',78,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('tyranoscript','TyranoScript',79,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('dos','MS-DOS',70,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('fds','Famicom Disk System',20,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('gba','Game Boy Advance',50,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('gbc','Game Boy / Color',40,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('kirikiri','KiriKiri',77,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('lynx','Atari Lynx',120,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('mastersystem','Master System',240,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('megadrive','Mega Drive / Genesis',130,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('n64','Nintendo 64',160,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('nds','Nintendo DS',80,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('nes','NES / Famicom',10,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('ngpc','Neo Geo Pocket / Color',150,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('nintendo3ds','Nintendo 3DS',250,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('ons','ONScripter',76,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('pce','PC Engine',140,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('pcfx','PC-FX',190,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('psp','PlayStation Portable',210,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('psx','PlayStation',170,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('rpgmaker','RPG Maker',75,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('saturn','Sega Saturn',180,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('snes','SNES',30,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('virtualboy','Virtual Boy',220,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('wasm4','WASM-4',81,1,0,0);
INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES('wonderswan','WonderSwan / Color',230,1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('a5200','Atari800 5200',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('azahar','Azahar',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('beetle_vb','Beetle VB',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('butterscotch','Butterscotch',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('tyranoscript','TyranoScript',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('desmume','DeSmuME',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('desmume2015','DeSmuME 2015',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('dosbox_pure','DOSBox Pure',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('fbalpha2012_cps1','FB Alpha 2012 CPS-1',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('fbalpha2012_cps2','FB Alpha 2012 CPS-2',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('fbneo','FinalBurn Neo',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('fceumm','FCEUmm',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('gambatte','Gambatte',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('genesis_plus_gx','Genesis Plus GX',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('genesis_plus_gx_wide','Genesis Plus GX Wide',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('handy','Handy',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('kirikiri2','KiriKiri2',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mame2003','MAME 2003',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mame2003_plus','MAME 2003 Plus',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mednafen_ngp','Beetle NeoPop',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mednafen_pce','Beetle PCE',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mednafen_pcfx','Beetle PC-FX',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mednafen_psx_hw','Beetle PSX HW',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mednafen_wswan','Beetle WonderSwan',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('melonds','melonDS',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mgba','mGBA',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('mupen64plus_next','Mupen64Plus-Next',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('nestopia','Nestopia UE',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('onscripter_yuri','ONScripter Yuri',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('opera','Opera',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('parallel_n64','ParaLLEl N64',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('pcsx_rearmed','PCSX-ReARMed',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('picodrive','PicoDrive',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('ppsspp','PPSSPP',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('prosystem','ProSystem',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('smsplus','SMS Plus GX',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('snes9x','Snes9x',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('stella2014','Stella 2014',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('wasm4','WASM-4',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('yabause','Yabause',1,0,0);
INSERT INTO cores(id,name,enabled,created_at_ms,updated_at_ms) VALUES('rpgmaker','RPG Maker',1,0,0);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('3do','opera',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('arcade','fbalpha2012_cps1',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('arcade','fbalpha2012_cps2',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('arcade','fbneo',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('arcade','mame2003',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('arcade','mame2003_plus',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('atari2600','stella2014',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('atari5200','a5200',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('atari7800','prosystem',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('butterscotch','butterscotch',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('tyranoscript','tyranoscript',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('dos','dosbox_pure',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('fds','fceumm',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('fds','nestopia',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('gba','mgba',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('gbc','gambatte',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('gbc','mgba',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('kirikiri','kirikiri2',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('lynx','handy',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('mastersystem','smsplus',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('megadrive','genesis_plus_gx',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('megadrive','genesis_plus_gx_wide',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('megadrive','picodrive',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('n64','mupen64plus_next',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('n64','parallel_n64',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('nds','desmume',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('nds','desmume2015',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('nds','melonds',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('nes','fceumm',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('nes','nestopia',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('ons','onscripter_yuri',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('ngpc','mednafen_ngp',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('nintendo3ds','azahar',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('pce','mednafen_pce',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('pcfx','mednafen_pcfx',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('psp','ppsspp',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('psx','mednafen_psx_hw',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('psx','pcsx_rearmed',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('rpgmaker','rpgmaker',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('saturn','yabause',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('snes','snes9x',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('virtualboy','beetle_vb',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('wasm4','wasm4',1);
INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES('wonderswan','mednafen_wswan',1);
