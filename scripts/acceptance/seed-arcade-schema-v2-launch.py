#!/usr/bin/env python3
"""Seed the persisted state that exposed the Arcade schema-v2 Launch regression.

The source game, content, DAT, Parent, and test BIOS blobs must already have been
created by arcade-flow.sh. This script only copies that immutable evidence into
a second published aggregate whose current revision deliberately remains the
review-produced schema-v2 snapshot and bypasses first-launch revalidation, as a
current screenshot-approved game does.
"""

from __future__ import annotations

import hashlib
import json
import sqlite3
import sys
import time
import uuid
from pathlib import Path


CORE_TITLES = {
    "mame2003": "MAME 2003 Schema V2 Regression",
    "fbneo": "FBNeo Schema V2 Regression",
}


def new_id() -> str:
    return str(uuid.uuid4())


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[2] not in CORE_TITLES:
        raise SystemExit("usage: seed-arcade-schema-v2-launch.py DATABASE mame2003|fbneo")
    database_path = Path(sys.argv[1]).resolve()
    core_id = sys.argv[2]
    title = CORE_TITLES[core_id]
    connection = sqlite3.connect(database_path)
    connection.execute("PRAGMA foreign_keys=ON")
    source = connection.execute(
        """
SELECT game.platform_instance_id,
       game.current_content_revision_id,
       revision.core_artifact_id,
       revision.dat_version_id,
       revision.dependency_snapshot_json
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN game_variants variant ON variant.game_id=game.id AND variant.core_id=?
JOIN game_variant_revisions revision ON revision.game_variant_id=variant.id
WHERE game.status='PUBLISHED'
  AND metadata.title='pacman'
  AND json_extract(revision.dependency_snapshot_json,'$.schemaVersion')=2
ORDER BY revision.created_at_ms DESC,revision.id DESC
LIMIT 1
""",
        (core_id,),
    ).fetchone()
    if source is None:
        raise SystemExit(f"no imported {core_id} Arcade schema-v2 revision is available")
    platform_instance_id, source_content_id, artifact_id, dat_version_id, snapshot_json = source
    snapshot = json.loads(snapshot_json)
    if (
        snapshot.get("schemaVersion") != 2
        or snapshot.get("datVersionId") != dat_version_id
        or not any(
            dependency.get("kind") == "PARENT"
            and dependency.get("machine") == "puckman"
            and dependency.get("state") == "SATISFIED_EXTERNAL"
            for dependency in snapshot.get("dependencies", [])
        )
        or not any(
            dependency.get("kind") == "BIOS_OR_BASE"
            and dependency.get("machine") == "retrombios"
            and dependency.get("state") == "SATISFIED_EXTERNAL"
            for dependency in snapshot.get("dependencies", [])
        )
    ):
        raise SystemExit("source Arcade revision does not contain the required schema-v2 Parent/BIOS evidence")
    roles = {
        row[0]
        for row in connection.execute(
            "SELECT role FROM variant_files WHERE game_variant_revision_id=(SELECT id FROM game_variant_revisions WHERE game_content_revision_id=? AND core_artifact_id=? AND json_extract(dependency_snapshot_json,'$.schemaVersion')=2 ORDER BY created_at_ms DESC,id DESC LIMIT 1)",
            (source_content_id, artifact_id),
        )
    }
    if not {"PARENT", "BIOS_BUNDLE"}.issubset(roles):
        raise SystemExit(f"source Arcade revision is missing frozen dependencies: {sorted(roles)}")

    game_id, metadata_id, content_id, variant_id, revision_id = (new_id() for _ in range(5))
    now = int(time.time() * 1000)
    digest = hashlib.sha256(f"{core_id}:{revision_id}:schema-v2".encode()).hexdigest()
    source_revision_id = connection.execute(
        """
SELECT id FROM game_variant_revisions
WHERE game_content_revision_id=? AND core_artifact_id=?
  AND json_extract(dependency_snapshot_json,'$.schemaVersion')=2
ORDER BY created_at_ms DESC,id DESC LIMIT 1
""",
        (source_content_id, artifact_id),
    ).fetchone()[0]

    connection.execute("BEGIN IMMEDIATE")
    connection.execute("PRAGMA defer_foreign_keys=ON")
    connection.execute(
        """
INSERT INTO game_metadata_revisions(
  id,game_id,title,title_initial,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,'A','Arcade schema-v2 runtime parser regression','','','',NULL,NULL,'ADMIN_EDIT',NULL,?)
""",
        (metadata_id, game_id, title, now),
    )
    connection.execute(
        """
INSERT INTO game_content_revisions(
  id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,
  source_manifest_digest,created_at_ms
)
SELECT ?,?,'SINGLE_FILE','ADMIN_REPLACE','schema-v2-regression',source_manifest_json,
       source_manifest_digest,?
FROM game_content_revisions WHERE id=?
""",
        (content_id, game_id, now, source_content_id),
    )
    connection.execute(
        """
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,'PUBLISHED',?,?,?,1,?,?)
""",
        (game_id, platform_instance_id, metadata_id, content_id, title.lower(), now, now),
    )
    connection.execute(
        """
INSERT INTO game_content_files(
  game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order
)
SELECT ?,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_content_files WHERE game_content_revision_id=?
""",
        (content_id, source_content_id),
    )
    connection.execute(
        """
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,NULL,1,?,?)
""",
        (variant_id, game_id, core_id, now, now),
    )
    emulator_game_id = connection.execute(
        "SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variant_revisions"
    ).fetchone()[0]
    connection.execute(
        """
INSERT INTO game_variant_revisions(
  id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,
  validation_input_digest,emulator_game_id,status,compatibility_code,
  dependency_snapshot_json,default_dos_entry,created_at_ms
) VALUES(?,?,?,?,?,?,?,'READY','REVIEW_SCREENSHOT_OVERRIDE',?,NULL,?)
""",
        (
            revision_id,
            variant_id,
            content_id,
            artifact_id,
            dat_version_id,
            digest,
            emulator_game_id,
            snapshot_json,
            now,
        ),
    )
    connection.execute(
        """
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
SELECT ?,role,logical_name,blob_id,sort_order FROM variant_files
WHERE game_variant_revision_id=?
""",
        (revision_id, source_revision_id),
    )
    connection.execute(
        """
INSERT INTO variant_dependencies(
  game_variant_revision_id,kind,logical_archive,dat_version_id,source_machine_name,
  required_entries_json,state,created_at_ms
)
SELECT ?,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,?
FROM variant_dependencies WHERE game_variant_revision_id=?
""",
        (revision_id, now, source_revision_id),
    )
    connection.execute(
        "UPDATE game_variants SET current_revision_id=? WHERE id=?",
        (revision_id, variant_id),
    )
    connection.commit()
    foreign_keys = connection.execute("PRAGMA foreign_key_check").fetchall()
    if foreign_keys:
        raise SystemExit(f"seeded Arcade schema-v2 game has foreign-key errors: {foreign_keys}")
    print(json.dumps({"gameId": game_id, "coreId": core_id, "title": title}, sort_keys=True))


if __name__ == "__main__":
    main()
