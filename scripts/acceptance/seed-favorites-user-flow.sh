#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -d "$database_path" ]]; then
  echo "usage: seed-favorites-user-flow.sh DATABASE" >&2
  exit 2
fi

python3 "$(dirname "${BASH_SOURCE[0]}")/postgres_fixture.py" query "$database_path" <<'SQL'

BEGIN;

INSERT INTO games(
  id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  metadata_source_kind,content_kind,content_source_kind,
  source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
) VALUES(
  '74100000-0000-7000-8000-000000000001',
  (SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
  'Favorite User Flow Game','F','Favorite acceptance','','','Fixture',NULL,1999,
  'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','[]',
  '0000000000000000000000000000000000000000000000000000000000007401',
  'PUBLISHED','RETAINED','favorite user flow game',1,1786003000001,1786003000001
);

COMMIT;
SQL

count="$(python3 "$(dirname "${BASH_SOURCE[0]}")/postgres_fixture.py" query "$database_path" "SELECT count(*) FROM games WHERE status='PUBLISHED';")"
if (( count < 2 )); then
  echo "favorite user-flow seed expected at least two published games, got: $count" >&2
  exit 1
fi
printf 'published_games=%s\n' "$count"
