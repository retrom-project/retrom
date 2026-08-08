ALTER TABLE bios_requirements
ADD COLUMN delivery_kind TEXT NOT NULL DEFAULT 'BIOS_BUNDLE'
CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE'));

ALTER TABLE bios_requirements
ADD COLUMN emulator_path TEXT;

CREATE TRIGGER bios_requirements_delivery_insert
BEFORE INSERT ON bios_requirements
WHEN NOT (
  (NEW.delivery_kind='BIOS_BUNDLE' AND NEW.emulator_path IS NULL) OR
  (NEW.delivery_kind='EXTERNAL_FILE' AND
   NEW.emulator_path IS NOT NULL AND
   length(NEW.emulator_path) BETWEEN 1 AND 512 AND
   substr(NEW.emulator_path,1,1)='/' AND
   NEW.emulator_path NOT LIKE '%\%' AND
   NEW.emulator_path NOT LIKE '%?%' AND
   NEW.emulator_path NOT LIKE '%#%' AND
   instr(NEW.emulator_path,char(0))=0 AND
   NEW.emulator_path NOT LIKE '%//%' AND
   NEW.emulator_path NOT LIKE '%/./%' AND
   NEW.emulator_path NOT LIKE '%/../%' AND
   NEW.emulator_path NOT LIKE '%/.' AND
   NEW.emulator_path NOT LIKE '%/..')
)
BEGIN
  SELECT RAISE(ABORT, 'invalid BIOS delivery');
END;

CREATE TRIGGER bios_requirements_delivery_update
BEFORE UPDATE OF delivery_kind,emulator_path ON bios_requirements
WHEN NOT (
  (NEW.delivery_kind='BIOS_BUNDLE' AND NEW.emulator_path IS NULL) OR
  (NEW.delivery_kind='EXTERNAL_FILE' AND
   NEW.emulator_path IS NOT NULL AND
   length(NEW.emulator_path) BETWEEN 1 AND 512 AND
   substr(NEW.emulator_path,1,1)='/' AND
   NEW.emulator_path NOT LIKE '%\%' AND
   NEW.emulator_path NOT LIKE '%?%' AND
   NEW.emulator_path NOT LIKE '%#%' AND
   instr(NEW.emulator_path,char(0))=0 AND
   NEW.emulator_path NOT LIKE '%//%' AND
   NEW.emulator_path NOT LIKE '%/./%' AND
   NEW.emulator_path NOT LIKE '%/../%' AND
   NEW.emulator_path NOT LIKE '%/.' AND
   NEW.emulator_path NOT LIKE '%/..')
)
BEGIN
  SELECT RAISE(ABORT, 'invalid BIOS delivery');
END;

CREATE TABLE launch_external_files (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  virtual_path TEXT NOT NULL CHECK(length(virtual_path) BETWEEN 1 AND 512),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 255),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  PRIMARY KEY(launch_session_id, virtual_path),
  UNIQUE(launch_session_id, logical_name),
  CHECK(substr(virtual_path,1,1)='/' AND
        virtual_path NOT LIKE '%\%' AND
        virtual_path NOT LIKE '%?%' AND
        virtual_path NOT LIKE '%#%' AND
        instr(virtual_path,char(0))=0 AND
        virtual_path NOT LIKE '%//%' AND
        virtual_path NOT LIKE '%/./%' AND
        virtual_path NOT LIKE '%/../%' AND
        virtual_path NOT LIKE '%/.' AND
        virtual_path NOT LIKE '%/..'),
  CHECK(logical_name NOT LIKE '%/%' AND
        logical_name NOT LIKE '%\%' AND
        logical_name NOT IN ('','.','..') AND
        instr(logical_name,char(0))=0)
);

CREATE INDEX fk_launch_external_files_blob ON launch_external_files(blob_id);

CREATE TRIGGER launch_external_files_immutable_update
BEFORE UPDATE ON launch_external_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_immutable_delete
BEFORE DELETE ON launch_external_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES
  ('nds','Nintendo DS',80,1,0,0),
  ('atari2600','Atari 2600',90,1,0,0),
  ('atari5200','Atari 5200',100,1,0,0),
  ('atari7800','Atari 7800',110,1,0,0),
  ('lynx','Atari Lynx',120,1,0,0),
  ('megadrive','Mega Drive / Genesis',130,1,0,0),
  ('pce','PC Engine',140,1,0,0),
  ('ngpc','Neo Geo Pocket / Color',150,1,0,0),
  ('n64','Nintendo 64',160,1,0,0),
  ('psx','PlayStation',170,1,0,0),
  ('saturn','Sega Saturn',180,1,0,0),
  ('pcfx','PC-FX',190,1,0,0),
  ('3do','3DO',200,1,0,0),
  ('psp','PlayStation Portable',210,1,0,0);

INSERT INTO cores(id,name,requires_threads,enabled,created_at_ms,updated_at_ms) VALUES
  ('nestopia','Nestopia UE',0,1,0,0),
  ('melonds','melonDS',0,1,0,0),
  ('desmume2015','DeSmuME 2015',0,1,0,0),
  ('desmume','DeSmuME',0,1,0,0),
  ('a5200','Atari800 5200',0,1,0,0),
  ('pcsx_rearmed','PCSX-ReARMed',0,1,0,0),
  ('mednafen_psx_hw','Beetle PSX HW',1,1,0,0),
  ('handy','Handy',0,1,0,0),
  ('yabause','Yabause',0,1,0,0),
  ('genesis_plus_gx','Genesis Plus GX',0,1,0,0),
  ('mupen64plus_next','Mupen64Plus-Next',0,1,0,0),
  ('parallel_n64','ParaLLEl N64',0,1,0,0),
  ('opera','Opera',0,1,0,0),
  ('prosystem','ProSystem',0,1,0,0),
  ('stella2014','Stella 2014',0,1,0,0),
  ('picodrive','PicoDrive',0,1,0,0),
  ('mednafen_pce','Beetle PCE',0,1,0,0),
  ('mednafen_pcfx','Beetle PC-FX',0,1,0,0),
  ('mednafen_ngp','Beetle NeoPop',0,1,0,0),
  ('ppsspp','PPSSPP',1,1,0,0);

INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES
  ('nes','nestopia',1),
  ('fds','nestopia',1),
  ('nds','melonds',1),
  ('nds','desmume2015',1),
  ('nds','desmume',1),
  ('atari5200','a5200',1),
  ('psx','pcsx_rearmed',1),
  ('psx','mednafen_psx_hw',1),
  ('lynx','handy',1),
  ('saturn','yabause',1),
  ('megadrive','genesis_plus_gx',1),
  ('megadrive','picodrive',1),
  ('n64','mupen64plus_next',1),
  ('n64','parallel_n64',1),
  ('3do','opera',1),
  ('atari7800','prosystem',1),
  ('atari2600','stella2014',1),
  ('pce','mednafen_pce',1),
  ('pcfx','mednafen_pcfx',1),
  ('ngpc','mednafen_ngp',1),
  ('psp','ppsspp',1);

INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,description,sort_order,enabled,version,created_at_ms,updated_at_ms) VALUES
  ('01980000-0000-7000-8000-000000000010','nds','desmume2015','Nintendo DS 游戏','nds-games','',100,1,1,0,0),
  ('01980000-0000-7000-8000-000000000011','atari2600','stella2014','Atari 2600 游戏','atari-2600-games','',110,1,1,0,0),
  ('01980000-0000-7000-8000-000000000012','atari5200','a5200','Atari 5200 游戏','atari-5200-games','',120,1,1,0,0),
  ('01980000-0000-7000-8000-000000000013','atari7800','prosystem','Atari 7800 游戏','atari-7800-games','',130,1,1,0,0),
  ('01980000-0000-7000-8000-000000000014','lynx','handy','Atari Lynx 游戏','atari-lynx-games','',140,1,1,0,0),
  ('01980000-0000-7000-8000-000000000015','megadrive','genesis_plus_gx','Mega Drive 游戏','mega-drive-games','',150,1,1,0,0),
  ('01980000-0000-7000-8000-000000000016','pce','mednafen_pce','PC Engine 游戏','pc-engine-games','',160,1,1,0,0),
  ('01980000-0000-7000-8000-000000000017','ngpc','mednafen_ngp','Neo Geo Pocket 游戏','neo-geo-pocket-games','',170,1,1,0,0),
  ('01980000-0000-7000-8000-000000000018','n64','mupen64plus_next','Nintendo 64 游戏','nintendo-64-games','',180,1,1,0,0),
  ('01980000-0000-7000-8000-000000000019','psx','pcsx_rearmed','PlayStation 游戏','playstation-games','',190,1,1,0,0),
  ('01980000-0000-7000-8000-000000000020','saturn','yabause','Sega Saturn 游戏','sega-saturn-games','',200,1,1,0,0),
  ('01980000-0000-7000-8000-000000000021','pcfx','mednafen_pcfx','PC-FX 游戏','pc-fx-games','',210,1,1,0,0),
  ('01980000-0000-7000-8000-000000000022','3do','opera','3DO 游戏','3do-games','',220,1,1,0,0),
  ('01980000-0000-7000-8000-000000000023','psp','ppsspp','PSP 游戏','psp-games','',230,1,1,0,0);
