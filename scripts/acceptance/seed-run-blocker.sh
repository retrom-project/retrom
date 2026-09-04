#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -f "$database_path" ]]; then
  echo "usage: seed-run-blocker.sh DATABASE" >&2
  exit 2
fi

sqlite3 -bail "$database_path" <<'SQL'
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;
CREATE TEMP TABLE acceptance_game AS
SELECT g.* FROM games g
WHERE g.status='PUBLISHED'
ORDER BY g.updated_at_ms DESC,g.id DESC
LIMIT 1;

INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
)
SELECT '60000000-0000-7000-8000-000000000001',
       (SELECT id FROM platform_instances WHERE catalog_template_key='nes/fceumm'),
       'Acceptance Missing FDS BIOS','A',description,developer,publisher,genre,players,release_year,
       'ADMIN_EDIT',NULL,content_kind,'ADMIN_REPLACE','acceptance-missing-fds-bios',
       source_manifest_json,source_manifest_digest,'PUBLISHED','RETAINED',
       'acceptance missing fds bios',1,1786000300000,1786000300000
FROM acceptance_game;

INSERT INTO game_files(
 game_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
)
SELECT '60000000-0000-7000-8000-000000000001',role,
       CASE WHEN role='CONTENT' THEN 'Acceptance-Missing-BIOS.fds' ELSE logical_name END,
       blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_files
WHERE game_id=(SELECT id FROM acceptance_game);

DROP TABLE acceptance_game;
COMMIT;
SQL

count="$(sqlite3 "$database_path" "SELECT count(*) FROM games WHERE id='60000000-0000-7000-8000-000000000001' AND status='PUBLISHED';")"
if [[ "$count" != "1" ]]; then
  echo "run blocker seed was not materialized" >&2
  exit 1
fi
printf 'blocked_game_id=60000000-0000-7000-8000-000000000001\n'
