#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -f "$database_path" ]]; then
  echo "usage: seed-favorites-layout.sh DATABASE" >&2
  exit 2
fi

sqlite3 -bail "$database_path" <<'SQL'
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;

CREATE TEMP TABLE favorite_owner AS
SELECT profile_id FROM users WHERE username='test' AND status='ENABLED' LIMIT 1;

DELETE FROM favorite_folder_games WHERE profile_id=(SELECT profile_id FROM favorite_owner);
DELETE FROM favorite_folders WHERE profile_id=(SELECT profile_id FROM favorite_owner);
DELETE FROM favorite_games WHERE profile_id=(SELECT profile_id FROM favorite_owner);

WITH RECURSIVE generated(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM generated WHERE n<50)
INSERT OR IGNORE INTO games(
  id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
  source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
)
SELECT printf('70000000-0000-7000-8000-%012d',n),
       (SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
       printf('Favorite Layout Game %02d',n),'F','Layout acceptance','','','Fixture',NULL,1980+n,
       'ADMIN_EDIT',NULL,'SINGLE_FILE','ADMIN_REPLACE','favorite-layout','[]',printf('%064x',n),
       'PUBLISHED','RETAINED',lower(printf('Favorite Layout Game %02d',n)),1,
       1786001000000+n,1786001000000+n
FROM generated;

WITH RECURSIVE generated(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM generated WHERE n<50)
INSERT OR IGNORE INTO favorite_games(profile_id,game_id,created_at_ms)
SELECT (SELECT profile_id FROM favorite_owner),printf('70000000-0000-7000-8000-%012d',n),1786001000000+n
FROM generated;

WITH RECURSIVE generated(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM generated WHERE n<100)
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
SELECT printf('73000000-0000-7000-8000-%012d',n),(SELECT profile_id FROM favorite_owner),
       printf('布局收藏夹 %03d',n),printf('布局收藏夹 %03d',n),1,1786002000000+n,1786002000000+n
FROM generated;

DROP TABLE favorite_owner;
COMMIT;
SQL

summary="$(sqlite3 "$database_path" "SELECT (SELECT count(*) FROM favorite_games fg JOIN users u ON u.profile_id=fg.profile_id WHERE u.username='test')||'/'||(SELECT count(*) FROM favorite_folders ff JOIN users u ON u.profile_id=ff.profile_id WHERE u.username='test');")"
if [[ "$summary" != 50/100 ]]; then
  echo "favorite layout seed mismatch: $summary" >&2
  exit 1
fi
printf 'favorite_games=%s\nfavorite_folders=%s\n' "${summary%/*}" "${summary#*/}"
