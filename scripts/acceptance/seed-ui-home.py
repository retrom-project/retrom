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

ROOT = Path(__file__).resolve().parents[2]
INDEX = 900
SAVE_ID = "0198ff00-9000-7000-8000-000000000001"
BLOB_ID = "0198ff00-9000-7000-8000-000000000002"


def seed(database_path: Path, state: str) -> None:
    if state not in {"empty", "played", "saved"}:
        raise ValueError("unknown UI home state")
    if not database_path.resolve().parent.parent.name.startswith("retrom-ui-acceptance."):
        raise ValueError("UI seed requires the disposable acceptance database")
    spec = importlib.util.spec_from_file_location("home_seed_support", Path(__file__).with_name("seed-immersive-library.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    with sqlite3.connect(database_path) as database:
        database.row_factory = sqlite3.Row
        database.execute("PRAGMA foreign_keys=ON")
        profile = database.execute("SELECT profile_id FROM users WHERE username='test'").fetchone()[0]
        game = module.base_game(database)
        launch_id = module.identifier(4, INDEX)
        database.execute("DELETE FROM save_states WHERE id=?", (SAVE_ID,))
        database.execute("DELETE FROM play_sessions WHERE id=?", (module.identifier(5, INDEX),))
        database.execute("DELETE FROM launch_sessions WHERE id=?", (launch_id,))
        if state == "empty":
            return
        timestamp = 1787600000000
        module.seed_play(database, profile, game["id"], game, INDEX, timestamp)
        if state == "played":
            return
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
            "VALUES(?,?,?,'ui-layout-fixture-v1',?,?,?,?,?,'UI layout fixture',1000,1,?,?)",
            (SAVE_ID, profile, game["id"], blob_id, digest, len(screenshot), blob_id, launch_id, timestamp, timestamp),
        )


if __name__ == "__main__":
    seed(Path(sys.argv[1]), sys.argv[2])
