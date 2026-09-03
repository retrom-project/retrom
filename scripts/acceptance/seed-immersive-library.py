#!/usr/bin/env python3
"""Seed deterministic metadata-only games for immersive pagination acceptance."""

from __future__ import annotations

import hashlib
import json
import sqlite3
import sys
from pathlib import Path


GAME_COUNT = 52
ID_PREFIX = "0198ff00-0000-7000-8000-"
FOLDER_ID = "0198ff00-1000-7000-8000-000000000001"
OTHER_PROFILE_ID = "0198ff00-2000-7000-8000-000000000001"
OTHER_FOLDER_ID = "0198ff00-2000-7000-8000-000000000002"


def identifier(group: int, index: int) -> str:
    return f"{ID_PREFIX}{group * 1000 + index:012x}"


def title_cases() -> list[tuple[str, str]]:
    special = [
        ("# Symbol acceptance", "#"),
        ("🎮 Emoji acceptance", "#"),
        ("0 Numeric acceptance", "0"),
        ("9 Numeric acceptance", "9"),
        ("alpha lowercase acceptance", "A"),
        ("Arcade uppercase acceptance", "A"),
        ("打击者验收", "D"),
        ("遊戲驗收", "Y"),
    ]
    fillers = [(f"Library acceptance {index:02d}", "L") for index in range(GAME_COUNT - len(special))]
    return special + fillers


def base_game(database: sqlite3.Connection) -> sqlite3.Row:
    row = database.execute(
        """
SELECT game.id AS game_id,
       game.platform_instance_id,
       game.current_content_revision_id,
       variant.core_id,
       revision.id AS variant_revision_id,
       revision.provider_id,
       revision.target_id,
       revision.target_contract_sha256,
       revision.game_compatibility_line,
       provider.bundle_sha256,
       revision.dat_version_id,
       revision.validation_input_digest,
       revision.compatibility_code,
       revision.dependency_snapshot_json,
       revision.default_dos_entry,
       content.source_manifest_json,
       content.source_manifest_digest
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
JOIN runtime_providers provider ON provider.provider_id=revision.provider_id
JOIN game_content_revisions content ON content.id=game.current_content_revision_id
WHERE game.status='PUBLISHED' AND metadata.title='Sudoku'
ORDER BY revision.created_at_ms DESC LIMIT 1
"""
    ).fetchone()
    if row is None:
        raise RuntimeError("IMMERSIVE_ACCEPTANCE_BASE_GAME_NOT_FOUND")
    return row


def seed_game(
    database: sqlite3.Connection,
    base: sqlite3.Row,
    index: int,
    title: str,
    title_initial: str,
    now_ms: int,
    emulator_game_id: int,
) -> tuple[str, str, str]:
    game_id = identifier(1, index)
    metadata_id = identifier(2, index)
    content_id = identifier(3, index)
    variant_id = identifier(6, index)
    variant_revision_id = identifier(7, index)
    database.execute(
        """
INSERT INTO game_metadata_revisions(
 id,game_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,?,?,'Retrom','','Acceptance',1,2026,'ADMIN_EDIT',NULL,?)
""",
        (metadata_id, game_id, title, title_initial, f"沉浸资料库分页验收条目 {index:02d}", now_ms),
    )
    database.execute(
        """
INSERT INTO game_content_revisions(
 id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?,'SINGLE_FILE','ADMIN_REPLACE',?,?,?,?)
""",
        (
            content_id,
            game_id,
            f"immersive-acceptance:{index}",
            base["source_manifest_json"],
            base["source_manifest_digest"],
            now_ms,
        ),
    )
    database.execute(
        """
INSERT INTO games(
 id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
 search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,'PUBLISHED',?,?,lower(?),1,?,?)
""",
        (game_id, base["platform_instance_id"], metadata_id, content_id, title, now_ms, now_ms),
    )
    for content_file in database.execute(
        """
SELECT role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_content_files WHERE game_content_revision_id=? ORDER BY role,logical_name
""",
        (base["current_content_revision_id"],),
    ).fetchall():
        database.execute(
            """
INSERT INTO game_content_files(
 game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,
 source_archive_entry_ordinal,sort_order
) VALUES(?,?,?,?,?,?,?)
""",
            (content_id, *content_file),
        )
    database.execute(
        """
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,NULL,1,?,?)
""",
        (variant_id, game_id, base["core_id"], now_ms, now_ms),
    )
    database.execute(
        """
INSERT INTO game_variant_revisions(
 id,game_variant_id,game_content_revision_id,provider_id,target_id,target_contract_sha256,
 game_compatibility_line,dat_version_id,
 validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,
 default_dos_entry,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
""",
        (
            variant_revision_id,
            variant_id,
            content_id,
            base["provider_id"],
            base["target_id"],
            base["target_contract_sha256"],
            base["game_compatibility_line"],
            base["dat_version_id"],
            base["validation_input_digest"],
            emulator_game_id,
            "READY",
            base["compatibility_code"],
            base["dependency_snapshot_json"],
            base["default_dos_entry"],
            now_ms,
        ),
    )
    database.execute(
        "UPDATE game_variants SET current_revision_id=? WHERE id=?",
        (variant_revision_id, variant_id),
    )
    return game_id, content_id, variant_revision_id


def seed_play(
    database: sqlite3.Connection,
    profile_id: str,
    game_id: str,
    content_revision_id: str,
    variant_revision_id: str,
    provider_id: str,
    target_id: str,
    target_contract_sha256: str,
    game_compatibility_line: str,
    bundle_sha256: str,
    index: int,
    started_at_ms: int,
) -> None:
    launch_id = identifier(4, index)
    play_id = identifier(5, index)
    database.execute(
        """
INSERT INTO launch_sessions(
 id,profile_id,purpose,game_id,game_content_revision_id,game_variant_revision_id,
 provider_id,target_id,target_contract_sha256,game_compatibility_line,bundle_sha256,
 save_state_id,dos_entry_path,
 return_to,credential_sha256,state,bootstrap_expires_at_ms,idle_expires_at_ms,activated_at_ms,
 finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version,initial_disc_index,
 netplay_session_id,netplay_player_no,save_access
) VALUES(?,?,'PRODUCT',?,?,?,?,?,?,?,?,NULL,NULL,'/immersive',?,'FINISHED',?,NULL,?,?,?,?,?,1,0,NULL,NULL,'NORMAL')
""",
        (
            launch_id,
            profile_id,
            game_id,
            content_revision_id,
            variant_revision_id,
            provider_id,
            target_id,
            target_contract_sha256,
            game_compatibility_line,
            bundle_sha256,
            hashlib.sha256(launch_id.encode()).digest(),
            started_at_ms + 60_000,
            started_at_ms,
            started_at_ms + 1_000,
            started_at_ms + 120_000,
            started_at_ms,
            started_at_ms + 1_000,
        ),
    )
    database.execute(
        """
INSERT INTO play_sessions(
 id,launch_session_id,profile_id,game_id,game_variant_revision_id,started_at_ms,
 last_heartbeat_at_ms,ended_at_ms,active_duration_ms,last_client_sequence,state,version,
 created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,1000,1,'FINISHED',1,?,?)
""",
        (
            play_id,
            launch_id,
            profile_id,
            game_id,
            variant_revision_id,
            started_at_ms,
            started_at_ms + 1_000,
            started_at_ms + 1_000,
            started_at_ms,
            started_at_ms + 1_000,
        ),
    )


def seed(database_path: Path) -> dict[str, object]:
    database = sqlite3.connect(database_path, timeout=30)
    database.row_factory = sqlite3.Row
    try:
        database.execute("PRAGMA foreign_keys=ON")
        database.execute("BEGIN IMMEDIATE")
        database.execute("PRAGMA defer_foreign_keys=ON")
        profile_id = database.execute("SELECT profile_id FROM users WHERE username='test'").fetchone()[0]
        existing = database.execute(
            "SELECT profile_id FROM favorite_folders WHERE id=?",
            (FOLDER_ID,),
        ).fetchone()
        if existing is not None:
            if existing["profile_id"] != profile_id:
                raise RuntimeError("IMMERSIVE_ACCEPTANCE_FOLDER_OWNER_MISMATCH")
            game_count = database.execute(
                "SELECT count(*) FROM favorite_folder_games WHERE profile_id=? AND folder_id=?",
                (profile_id, FOLDER_ID),
            ).fetchone()[0]
            if game_count != GAME_COUNT:
                raise RuntimeError("IMMERSIVE_ACCEPTANCE_SEED_INCOMPLETE")
            database.commit()
            return {
                "folderId": FOLDER_ID,
                "gameCount": game_count,
                "otherProfileId": OTHER_PROFILE_ID,
                "profileId": profile_id,
            }
        base = base_game(database)
        latest_play = database.execute("SELECT coalesce(max(started_at_ms),1787600000000) FROM play_sessions").fetchone()[0]
        next_emulator_game_id = database.execute(
            "SELECT coalesce(max(emulator_game_id),0)+1 FROM game_variant_revisions"
        ).fetchone()[0]
        database.execute(
            "INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms) "
            "VALUES(?,?,'验收分页','验收分页',1,?,?)",
            (FOLDER_ID, profile_id, latest_play, latest_play),
        )
        database.execute(
            "INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Immersive Other',?)",
            (OTHER_PROFILE_ID, latest_play),
        )
        database.execute(
            "INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms) "
            "VALUES(?,?,'另一玩家私有','另一玩家私有',1,?,?)",
            (OTHER_FOLDER_ID, OTHER_PROFILE_ID, latest_play, latest_play),
        )
        game_ids: list[str] = []
        for index, (title, title_initial) in enumerate(title_cases(), start=1):
            timestamp = latest_play + index
            game_id, content_revision_id, variant_revision_id = seed_game(
                database,
                base,
                index,
                title,
                title_initial,
                timestamp,
                next_emulator_game_id + index - 1,
            )
            game_ids.append(game_id)
            seed_play(
                database,
                profile_id,
                game_id,
                content_revision_id,
                variant_revision_id,
                base["provider_id"],
                base["target_id"],
                base["target_contract_sha256"],
                base["game_compatibility_line"],
                base["bundle_sha256"],
                index,
                timestamp,
            )
            database.execute(
                "INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,?)",
                (profile_id, game_id, timestamp),
            )
            database.execute(
                "INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,?)",
                (profile_id, FOLDER_ID, game_id, timestamp),
            )
        seed_play(
            database,
            profile_id,
            base["game_id"],
            base["current_content_revision_id"],
            base["variant_revision_id"],
            base["provider_id"],
            base["target_id"],
            base["target_contract_sha256"],
            base["game_compatibility_line"],
            base["bundle_sha256"],
            GAME_COUNT + 1,
            latest_play + GAME_COUNT + 1,
        )
        database.execute(
            "INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,?)",
            (OTHER_PROFILE_ID, game_ids[0], latest_play),
        )
        database.execute(
            "INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,?)",
            (OTHER_PROFILE_ID, OTHER_FOLDER_ID, game_ids[0], latest_play),
        )
        database.commit()
        return {
            "folderId": FOLDER_ID,
            "gameCount": len(game_ids),
            "otherProfileId": OTHER_PROFILE_ID,
            "profileId": profile_id,
        }
    finally:
        database.close()


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: seed-immersive-library.py DATABASE")
    print(json.dumps(seed(Path(sys.argv[1])), ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    main()
