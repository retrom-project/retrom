#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -d "$database_path" ]]; then
  echo "usage: seed-favorites-layout.sh DATABASE" >&2
  exit 2
fi

python3 "$(dirname "${BASH_SOURCE[0]}")/postgres_fixture.py" query "$database_path" <<'SQL'

BEGIN;

CREATE TEMP TABLE favorite_owner AS
SELECT profile_id FROM users WHERE username='test' AND status='ENABLED' LIMIT 1;

DELETE FROM favorite_folder_games WHERE profile_id=(SELECT profile_id FROM favorite_owner);
DELETE FROM favorite_folders WHERE profile_id=(SELECT profile_id FROM favorite_owner);
DELETE FROM favorite_games WHERE profile_id=(SELECT profile_id FROM favorite_owner);

WITH RECURSIVE generated(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM generated WHERE n<50)
INSERT INTO games(
  id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  metadata_source_kind,content_kind,content_source_kind,
  source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
)
SELECT concat('70000000-0000-7000-8000-',lpad((n)::text,12,'0')),
       (SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
       concat('Favorite Layout Game ',lpad((n)::text,2,'0')),'F','Layout acceptance','','','Fixture',NULL,1980+n,
       'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','[]',concat(lpad(to_hex((n)::bigint),64,'0')),
       'PUBLISHED','RETAINED',lower(concat('Favorite Layout Game ',lpad((n)::text,2,'0'))),1,
       1786001000000+n,1786001000000+n
FROM generated ON CONFLICT DO NOTHING;

WITH RECURSIVE generated(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM generated WHERE n<50)
INSERT INTO favorite_games(profile_id,game_id,created_at_ms)
SELECT (SELECT profile_id FROM favorite_owner),concat('70000000-0000-7000-8000-',lpad((n)::text,12,'0')),1786001000000+n
FROM generated ON CONFLICT DO NOTHING;

WITH RECURSIVE generated(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM generated WHERE n<100)
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
SELECT concat('73000000-0000-7000-8000-',lpad((n)::text,12,'0')),(SELECT profile_id FROM favorite_owner),
       concat('布局收藏夹 ',lpad((n)::text,3,'0')),concat('布局收藏夹 ',lpad((n)::text,3,'0')),1,1786002000000+n,1786002000000+n
FROM generated;

DROP TABLE favorite_owner;
COMMIT;
SQL

summary="$(python3 "$(dirname "${BASH_SOURCE[0]}")/postgres_fixture.py" query "$database_path" "SELECT (SELECT count(*) FROM favorite_games fg JOIN users u ON u.profile_id=fg.profile_id WHERE u.username='test')||'/'||(SELECT count(*) FROM favorite_folders ff JOIN users u ON u.profile_id=ff.profile_id WHERE u.username='test');")"
if [[ "$summary" != 50/100 ]]; then
  echo "favorite layout seed mismatch: $summary" >&2
  exit 1
fi
printf 'favorite_games=%s\nfavorite_folders=%s\n' "${summary%/*}" "${summary#*/}"
