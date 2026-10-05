#!/usr/bin/env python3
"""Clone an imported Arcade game while preserving its current dependency snapshot."""

from __future__ import annotations

import hashlib
import json
import postgres_fixture as pg
import sys
import time
from pathlib import Path
from fixture_files import own_rows


CORE_TITLES = {
    "mame2003": "MAME 2003 Current Snapshot Regression",
    "fbneo": "FBNeo Current Snapshot Regression",
}


def fixture_id(core_id: str, role: str) -> str:
    suffix = hashlib.sha256(f"arcade-current:{core_id}:{role}".encode()).hexdigest()
    return f"0198ff02-{suffix[:4]}-7{suffix[4:7]}-8{suffix[7:10]}-{suffix[10:22]}"


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[2] not in CORE_TITLES:
        raise SystemExit("usage: seed-arcade-current-launch.py DATABASE mame2003|fbneo")
    database_path = Path(sys.argv[1]).resolve()
    core_id = sys.argv[2]
    title = CORE_TITLES[core_id]
    connection = pg.connect(database_path)
    connection.row_factory = pg.row_factory
    connection.execute("SET LOCAL lock_timeout='30s'")
    source = connection.execute(
        """
SELECT game.*,variant.id AS variant_id,variant.provider_id,variant.target_id,
       variant.dat_version_id,variant.compatibility_code,variant.dependency_snapshot_json,
       variant.default_dos_entry
FROM games game
JOIN game_variants variant ON variant.game_id=game.id AND variant.core_id=%s
WHERE game.status='PUBLISHED' AND game.title='pacman'
  AND variant.status='READY'
  AND (((variant.dependency_snapshot_json)::jsonb #>> '{schemaVersion}'))::bigint=1
  AND ((variant.dependency_snapshot_json)::jsonb #>> '{kind}')='ARCADE'
ORDER BY variant.updated_at_ms DESC,variant.id DESC
LIMIT 1
""",
        (core_id,),
    ).fetchone()
    if source is None:
        raise SystemExit(f"no imported {core_id} Arcade current game is available")
    snapshot = json.loads(source["dependency_snapshot_json"])
    dependencies = snapshot.get("dependencies", [])
    required = {
        (item.get("kind"), item.get("machine"), item.get("state"))
        for item in dependencies if isinstance(item, dict)
    }
    if ("PARENT", "puckman", "SATISFIED_EXTERNAL") not in required or \
            ("BIOS_OR_BASE", "retrombios", "SATISFIED_EXTERNAL") not in required:
        raise SystemExit("source Arcade game lacks the required current Parent/BIOS evidence")
    roles = {
        row[0] for row in connection.execute(
            "SELECT role FROM variant_files WHERE game_variant_id=%s", (source["variant_id"],)
        )
    }
    if not {"PARENT", "BIOS_BUNDLE"}.issubset(roles):
        raise SystemExit(f"source Arcade game is missing frozen dependencies: {sorted(roles)}")

    game_id, variant_id = fixture_id(core_id, "game"), fixture_id(core_id, "variant")
    now = int(time.time() * 1000)
    emulator_game_id = connection.execute(
        "SELECT COALESCE(max(emulator_game_id),1000)+1 FROM game_variants"
    ).fetchone()[0]
    connection.execute("BEGIN")
    connection.execute(
        """
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,content_kind,content_source_kind,
 source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
) VALUES(%s,%s,%s,'M','Arcade current runtime parser regression','','','',NULL,NULL,
 'ADMIN_EDIT',%s,'ADMIN_REPLACE',%s,%s,'PUBLISHED','RETAINED',lower(%s),1,%s,%s)
""",
        (
            game_id, source["platform_instance_id"], title, source["content_kind"],
            source["source_manifest_json"], source["source_manifest_digest"], title, now, now,
        ),
    )
    connection.execute(
        """
INSERT INTO game_files(
 game_id,role,logical_name,file_record,source_archive_file_record,source_archive_entry_ordinal,sort_order
)
SELECT %s,role,logical_name,file_record,source_archive_file_record,source_archive_entry_ordinal,sort_order
FROM game_files WHERE game_id=%s
""",
        (game_id, source["id"]),
    )
    connection.execute(
        """
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,status,
 compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms
) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
""",
        (
            variant_id, game_id, core_id, source["provider_id"], source["target_id"],
            source["dat_version_id"], emulator_game_id, "READY", "REVIEW_SCREENSHOT_OVERRIDE",
            source["dependency_snapshot_json"], source["default_dos_entry"], 1, now, now,
        ),
    )
    connection.execute(
        """
INSERT INTO variant_files(game_variant_id,role,logical_name,file_record,sort_order)
SELECT %s,role,logical_name,file_record,sort_order FROM variant_files WHERE game_variant_id=%s
""",
        (variant_id, source["variant_id"]),
    )
    connection.execute(
        """
INSERT INTO variant_dependencies(
 game_variant_id,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,created_at_ms
)
SELECT %s,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,%s
FROM variant_dependencies WHERE game_variant_id=%s
""",
        (variant_id, now, source["variant_id"]),
    )
    own_rows(connection, "game_files", "game_id", game_id, "GAME", game_id)
    own_rows(connection, "variant_files", "game_variant_id", variant_id, "GAME", game_id, extra="AND role<>'BIOS_BUNDLE'")
    connection.commit()
    foreign_keys = connection.execute("SELECT conname FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND NOT convalidated").fetchall()
    if foreign_keys:
        raise SystemExit(f"seeded Arcade current game has foreign-key errors: {foreign_keys}")
    print(json.dumps({"gameId": game_id, "coreId": core_id, "title": title}, sort_keys=True))


if __name__ == "__main__":
    main()
