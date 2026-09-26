#!/usr/bin/env python3
"""Prepare UI-only home states in the disposable browser acceptance database.

The screenshot is a public fixture; the checkpoint is for layout only and is
never launched. This command refuses persistent development databases.
"""
from __future__ import annotations

import hashlib
import importlib.util
import sqlite3
import sys
import zlib
from pathlib import Path
from ui_layout_state import validate_database

ROOT = Path(__file__).resolve().parents[2]
INDEX = 900
SAVE_ID = "0198ff00-9000-7000-8000-000000000001"
BLOB_ID = "0198ff00-9000-7000-8000-000000000002"


def seed(database_path: Path, state: str) -> None:
    if state not in {"empty", "played", "saved", "populated"}:
        raise ValueError("unknown UI home state")
    validate_database(database_path)
    spec = importlib.util.spec_from_file_location("home_seed_support", Path(__file__).with_name("seed-immersive-library.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    with sqlite3.connect(database_path) as database:
        database.row_factory = sqlite3.Row
        database.execute("PRAGMA foreign_keys=ON")
        profile = database.execute("SELECT profile_id FROM users WHERE username='test'").fetchone()[0]
        game = module.base_game(database)
        database.execute("DELETE FROM save_states WHERE id=?", (SAVE_ID,))
        for index in (INDEX, INDEX + 1):
            database.execute("DELETE FROM play_sessions WHERE id=?", (module.identifier(5, index),))
            database.execute("DELETE FROM launch_sessions WHERE id=?", (module.identifier(4, index),))
        launch_id = module.identifier(4, INDEX)
        if state == "empty":
            return
        timestamp = 1787600000000
        module.seed_play(database, profile, game["id"], game, INDEX, timestamp)
        if state == "populated":
            seed_recent_poster(database, module, profile, game, timestamp)
            return
        if state == "played":
            return
        checkpoint_format = database.execute(
            "SELECT json_extract(checkpoint_json,'$.readFormats[0]') FROM runtime_targets WHERE provider_id=? AND target_id=?",
            (game["provider_id"], game["target_id"]),
        ).fetchone()[0]
        if not checkpoint_format:
            raise ValueError("UI fixture target must support checkpoints")
        screenshot = (ROOT / "testdata/public-roms/gba-smoke/emulationstation-smoke-cover.png").read_bytes()
        digest = hashlib.sha256(screenshot).hexdigest()
        target = database_path.parent / "blobs/sha256" / digest[:2] / digest[2:4] / digest
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(screenshot)
        database.execute(
            "INSERT OR IGNORE INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) "
            "VALUES(?,?,?,?,?,?,'image/png',?)",
            (BLOB_ID, digest, len(screenshot), hashlib.md5(screenshot).hexdigest(),
             hashlib.sha1(screenshot).hexdigest(), f"{zlib.crc32(screenshot):08x}", timestamp),
        )
        blob_id = database.execute("SELECT id FROM blobs WHERE sha256=?", (digest,)).fetchone()[0]
        database.execute(
            "INSERT INTO save_states(id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,"
            "payload_size_bytes,screenshot_blob_id,source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms) "
            "VALUES(?,?,?,?,?,?,?,?,?,'UI layout fixture',1000,1,?,?)",
            (SAVE_ID, profile, game["id"], checkpoint_format, blob_id, digest, len(screenshot), blob_id, launch_id, timestamp, timestamp),
        )


def seed_recent_poster(database, module, profile, game, timestamp):
    """A second public-fixture game keeps a measurable poster after hero deduplication."""
    index = INDEX + 1
    existing = database.execute(
        "SELECT g.*,v.core_id,v.provider_id,v.target_id,v.dependency_snapshot_json,v.compatibility_code,p.bundle_sha256 "
        "FROM games g JOIN game_variants v ON v.game_id=g.id AND v.status='READY' "
        "JOIN runtime_providers p ON p.provider_id=v.provider_id "
        "WHERE g.status='PUBLISHED' AND g.source_manifest_digest<>? ORDER BY g.id LIMIT 1",
        (game["source_manifest_digest"],),
    ).fetchone()
    if existing is not None:
        module.seed_play(database, profile, existing["id"], existing, index, timestamp - 2000)
        return
    recent_id = module.identifier(1, index)
    if database.execute("SELECT 1 FROM games WHERE id=?", (recent_id,)).fetchone():
        module.seed_play(database, profile, recent_id, game, index, timestamp - 2000)
        return
    recent_id, _ = module.seed_game(
        database, game, index, "Homepage poster acceptance", "H", timestamp - 2000, index,
    )
    database.execute(
        "INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms) "
        "SELECT ?,?,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms "
        "FROM game_assets WHERE game_id=? AND kind='COVER' AND ordinal=0",
        (module.identifier(7, index), recent_id, game["id"]),
    )
    module.seed_play(database, profile, recent_id, game, index, timestamp - 2000)


if __name__ == "__main__":
    seed(Path(sys.argv[1]), sys.argv[2])
