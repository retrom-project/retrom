#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -f "$database_path" ]]; then
  echo "usage: seed-favorites-user-flow.sh DATABASE" >&2
  exit 2
fi

sqlite3 -bail "$database_path" <<'SQL'
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;
PRAGMA defer_foreign_keys=ON;

INSERT INTO game_metadata_revisions(
  id,game_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(
  '74000000-0000-7000-8000-000000000001',
  '74100000-0000-7000-8000-000000000001',
  'Favorite User Flow Game','F','Favorite acceptance','','','Fixture',NULL,1999,
  'ADMIN_EDIT',NULL,1786003000001
);

INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(
  '74200000-0000-7000-8000-000000000001',
  '74100000-0000-7000-8000-000000000001',
  'ADMIN_REPLACE','favorite-user-flow','[]',
  '0000000000000000000000000000000000000000000000000000000000007401',
  1786003000001
);

INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(
  '74100000-0000-7000-8000-000000000001',
  (SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
  'PUBLISHED',
  '74000000-0000-7000-8000-000000000001',
  '74200000-0000-7000-8000-000000000001',
  'favorite user flow game',1,1786003000001,1786003000001
);

COMMIT;
SQL

count="$(sqlite3 "$database_path" "SELECT count(*) FROM games WHERE status='PUBLISHED';")"
if (( count < 2 )); then
  echo "favorite user-flow seed expected at least two published games, got: $count" >&2
  exit 1
fi
printf 'published_games=%s\n' "$count"
