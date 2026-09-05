#!/usr/bin/env python3
"""Clone an imported Arcade game while preserving its current dependency snapshot."""

from __future__ import annotations

import json
import sqlite3
import sys
import time
import uuid
from pathlib import Path


CORE_TITLES = {
    "mame2003": "MAME 2003 Current Snapshot Regression",
    "fbneo": "FBNeo Current Snapshot Regression",
}


def new_id() -> str:
    return str(uuid.uuid4())


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[2] not in CORE_TITLES:
        raise SystemExit("usage: seed-arcade-current-launch.py DATABASE mame2003|fbneo")
    database_path = Path(sys.argv[1]).resolve()
    core_id = sys.argv[2]
    title = CORE_TITLES[core_id]
    connection = sqlite3.connect(database_path)
    connection.row_factory = sqlite3.Row
    connection.execute("PRAGMA foreign_keys=ON")
    connection.execute("PRAGMA busy_timeout=30000")
    source = connection.execute(
        """
SELECT game.*,variant.id AS variant_id,variant.provider_id,variant.target_id,
       variant.dat_version_id,variant.compatibility_code,variant.dependency_snapshot_json,
       variant.default_dos_entry
FROM games game
JOIN game_variants variant ON variant.game_id=game.id AND variant.core_id=?
WHERE game.status='PUBLISHED' AND game.title='pacman'
  AND variant.status='READY'
  AND json_extract(variant.dependency_snapshot_json,'$.schemaVersion')=1
  AND json_extract(variant.dependency_snapshot_json,'$.kind')='ARCADE'
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
            "SELECT role FROM variant_files WHERE game_variant_id=?", (source["variant_id"],)
        )
    }
    if not {"PARENT", "BIOS_BUNDLE"}.issubset(roles):
        raise SystemExit(f"source Arcade game is missing frozen dependencies: {sorted(roles)}")

    game_id, variant_id = new_id(), new_id()
    now = int(time.time() * 1000)
    emulator_game_id = connection.execute(
        "SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variants"
    ).fetchone()[0]
    connection.execute("BEGIN IMMEDIATE")
    connection.execute(
        """
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,payload_state,search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,'M','Arcade current runtime parser regression','','','',NULL,NULL,
 'ADMIN_EDIT',NULL,?,'ADMIN_REPLACE','current-regression',?,?,'PUBLISHED','RETAINED',lower(?),1,?,?)
""",
        (
            game_id, source["platform_instance_id"], title, source["content_kind"],
            source["source_manifest_json"], source["source_manifest_digest"], title, now, now,
        ),
    )
    connection.execute(
        """
INSERT INTO game_files(
 game_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
)
SELECT ?,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_files WHERE game_id=?
""",
        (game_id, source["id"]),
    )
    connection.execute(
        """
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,status,
 compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
""",
        (
            variant_id, game_id, core_id, source["provider_id"], source["target_id"],
            source["dat_version_id"], emulator_game_id, "READY", "REVIEW_SCREENSHOT_OVERRIDE",
            source["dependency_snapshot_json"], source["default_dos_entry"], 1, now, now,
        ),
    )
    connection.execute(
        """
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
SELECT ?,role,logical_name,blob_id,sort_order FROM variant_files WHERE game_variant_id=?
""",
        (variant_id, source["variant_id"]),
    )
    connection.execute(
        """
INSERT INTO variant_dependencies(
 game_variant_id,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,created_at_ms
)
SELECT ?,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,?
FROM variant_dependencies WHERE game_variant_id=?
""",
        (variant_id, now, source["variant_id"]),
    )
    connection.commit()
    foreign_keys = connection.execute("PRAGMA foreign_key_check").fetchall()
    if foreign_keys:
        raise SystemExit(f"seeded Arcade current game has foreign-key errors: {foreign_keys}")
    print(json.dumps({"gameId": game_id, "coreId": core_id, "title": title}, sort_keys=True))


if __name__ == "__main__":
    main()
