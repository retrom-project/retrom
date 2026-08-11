INSERT INTO platforms(id,name,sort_order,enabled,created_at_ms,updated_at_ms) VALUES
  ('virtualboy','Virtual Boy',220,1,0,0),
  ('wonderswan','WonderSwan / Color',230,1,0,0),
  ('mastersystem','Master System',240,1,0,0),
  ('nintendo3ds','Nintendo 3DS',250,1,0,0);

INSERT INTO cores(id,name,requires_threads,enabled,created_at_ms,updated_at_ms) VALUES
  ('beetle_vb','Beetle VB',0,1,0,0),
  ('mednafen_wswan','Beetle WonderSwan',0,1,0,0),
  ('smsplus','SMS Plus GX',0,1,0,0),
  ('fbalpha2012_cps1','FB Alpha 2012 CPS-1',0,1,0,0),
  ('fbalpha2012_cps2','FB Alpha 2012 CPS-2',0,1,0,0),
  ('genesis_plus_gx_wide','Genesis Plus GX Wide',0,1,0,0),
  ('azahar','Azahar',1,1,0,0);

INSERT INTO platform_cores(platform_id,core_id,enabled) VALUES
  ('virtualboy','beetle_vb',1),
  ('wonderswan','mednafen_wswan',1),
  ('mastersystem','smsplus',1),
  ('arcade','fbalpha2012_cps1',1),
  ('arcade','fbalpha2012_cps2',1),
  ('megadrive','genesis_plus_gx_wide',1),
  ('nintendo3ds','azahar',1);

INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,description,sort_order,enabled,version,created_at_ms,updated_at_ms) VALUES
  ('01980000-0000-7000-8000-000000000024','virtualboy','beetle_vb','Virtual Boy 游戏','virtual-boy-games','',240,1,1,0,0),
  ('01980000-0000-7000-8000-000000000025','wonderswan','mednafen_wswan','WonderSwan 游戏','wonderswan-games','',250,1,1,0,0),
  ('01980000-0000-7000-8000-000000000026','mastersystem','smsplus','Master System 游戏','master-system-games','',260,1,1,0,0),
  ('01980000-0000-7000-8000-000000000027','arcade','fbalpha2012_cps1','FB Alpha 2012 CPS-1 游戏','fbalpha2012-cps1-games','',270,1,1,0,0),
  ('01980000-0000-7000-8000-000000000028','arcade','fbalpha2012_cps2','FB Alpha 2012 CPS-2 游戏','fbalpha2012-cps2-games','',280,1,1,0,0),
  ('01980000-0000-7000-8000-000000000029','nintendo3ds','azahar','Nintendo 3DS 游戏','nintendo-3ds-games','',290,1,1,0,0);
