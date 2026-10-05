#!/usr/bin/env python3
"""Create an independent missing-BIOS game in disposable acceptance data."""
import postgres_fixture as pg
import sys
from fixture_files import own_rows

with pg.connect(sys.argv[1]) as db:
    db.execute(r"""

BEGIN;
CREATE TEMP TABLE acceptance_game AS
SELECT g.* FROM games g
WHERE g.status='PUBLISHED'
ORDER BY g.updated_at_ms DESC,g.id DESC
LIMIT 1;

INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,content_kind,content_source_kind,
 source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
)
SELECT '60000000-0000-7000-8000-000000000001',
       (SELECT id FROM platform_instances WHERE catalog_template_key='nes/fceumm'),
       'Acceptance Missing FDS BIOS','A',description,developer,publisher,genre,players,release_year,
       'ADMIN_EDIT',content_kind,'ADMIN_REPLACE',
       source_manifest_json,source_manifest_digest,'PUBLISHED','RETAINED',
       'acceptance missing fds bios',1,1786000300000,1786000300000
FROM acceptance_game;

INSERT INTO game_files(
 game_id,role,logical_name,file_record,source_archive_file_record,source_archive_entry_ordinal,sort_order
)
SELECT '60000000-0000-7000-8000-000000000001',role,
       CASE WHEN role='CONTENT' THEN 'Acceptance-Missing-BIOS.fds' ELSE logical_name END,
       file_record,source_archive_file_record,source_archive_entry_ordinal,sort_order
FROM game_files
WHERE game_id=(SELECT id FROM acceptance_game);

DROP TABLE acceptance_game;

""")
    own_rows(db, "game_files", "game_id", "60000000-0000-7000-8000-000000000001", "GAME", "60000000-0000-7000-8000-000000000001")
print("blocked_game_id=60000000-0000-7000-8000-000000000001")
