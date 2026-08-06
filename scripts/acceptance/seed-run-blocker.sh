#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -f "$database_path" ]]; then
  echo "usage: seed-run-blocker.sh DATABASE" >&2
  exit 2
fi

sqlite3 -bail "$database_path" <<'SQL'
PRAGMA foreign_keys=ON;
PRAGMA defer_foreign_keys=ON;
BEGIN IMMEDIATE;
CREATE TEMP TABLE acceptance_game AS
SELECT g.id AS game_id,
       g.current_metadata_revision_id AS metadata_id,
       g.current_content_revision_id AS content_id
FROM games g
WHERE g.status='PUBLISHED'
ORDER BY g.updated_at_ms DESC,g.id DESC
LIMIT 1;

INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,created_at_ms)
SELECT '60000000-0000-7000-8000-000000000002',
       '60000000-0000-7000-8000-000000000001',
       'Acceptance Missing FDS BIOS',description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,1786000300000
FROM game_metadata_revisions
WHERE id=(SELECT metadata_id FROM acceptance_game);

INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
SELECT '60000000-0000-7000-8000-000000000003',
       '60000000-0000-7000-8000-000000000001',source_kind,source_ref_id,source_manifest_json,source_manifest_digest,1786000300000
FROM game_content_revisions
WHERE id=(SELECT content_id FROM acceptance_game);

INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order)
SELECT '60000000-0000-7000-8000-000000000003',role,
       CASE WHEN role='CONTENT' THEN 'Acceptance-Missing-BIOS.fds' ELSE logical_name END,
       blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_content_files
WHERE game_content_revision_id=(SELECT content_id FROM acceptance_game);

INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms,deleted_at_ms)
VALUES('60000000-0000-7000-8000-000000000001','01980000-0000-7000-8000-000000000002','PUBLISHED',
       '60000000-0000-7000-8000-000000000002','60000000-0000-7000-8000-000000000003',
       'acceptance missing fds bios',1,1786000300000,1786000300000,NULL);

DROP TABLE acceptance_game;
COMMIT;
SQL

count="$(sqlite3 "$database_path" "SELECT count(*) FROM games WHERE id='60000000-0000-7000-8000-000000000001' AND status='PUBLISHED';")"
if [[ "$count" != "1" ]]; then
  echo "run blocker seed was not materialized" >&2
  exit 1
fi
printf 'blocked_game_id=60000000-0000-7000-8000-000000000001\n'
