#!/usr/bin/env python3
"""Seed detail layout states only in the disposable UI acceptance database.

Uses the public screenshot fixture as a layout-only payload; never launch it.
"""
import importlib.util
import hashlib
import sqlite3
import sys
import zlib
from pathlib import Path
from fixture_references import adjust_references
from ui_layout_state import validate_database


def ensure_video(db, database_path, game_id, timestamp):
    if db.execute("SELECT 1 FROM game_assets WHERE game_id=? AND kind='VIDEO'", (game_id,)).fetchone():
        return
    root = Path(__file__).resolve().parents[2]
    contents = (root / "testdata/public-roms/gba-smoke/emulationstation-smoke-video.webm").read_bytes()
    digest = hashlib.sha256(contents).hexdigest()
    target = database_path.parent / "blobs/sha256" / digest[:2] / digest[2:4] / digest
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.exists():
        if target.read_bytes() != contents:
            raise ValueError("layout video CAS content differs from the public fixture")
    else:
        target.write_bytes(contents)
    db.execute(
        "INSERT OR IGNORE INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) "
        "VALUES('0198ff00-9002-7000-8000-000000000001',?,?,?,?,?,'video/webm',?)",
        (digest, len(contents), hashlib.md5(contents).hexdigest(), hashlib.sha1(contents).hexdigest(),
         f"{zlib.crc32(contents):08x}", timestamp),
    )
    blob_id = db.execute("SELECT id FROM blobs WHERE sha256=?", (digest,)).fetchone()[0]
    db.execute(
        "INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,media_type,created_at_ms) "
        "VALUES('0198ff00-9002-7000-8000-000000000002',?,?,'VIDEO',0,'video/webm',?)",
        (game_id, blob_id, timestamp),
    )
    adjust_references(db, [(blob_id,)])


def seed(path: Path) -> str:
    validate_database(path)
    spec = importlib.util.spec_from_file_location("ui_home_seed", Path(__file__).with_name("seed-ui-home.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.seed(path, "saved")
    with sqlite3.connect(path) as db:
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("BEGIN IMMEDIATE")
        original = dict(db.execute("SELECT * FROM save_states WHERE id=?", (module.SAVE_ID,)).fetchone())
        # Another viewport's media lifecycle case intentionally removes the video.
        # Layout acceptance owns this prerequisite instead of depending on test order.
        ensure_video(db, path, original["game_id"], original["created_at_ms"])
        for index in range(1, 4):
            row = {**original, "id": f"0198ff00-9001-7000-8000-{index:012d}", "name": f"详情布局存档 {index}",
                   "created_at_ms": original["created_at_ms"] - index * 60000}
            if index == 2:
                row["screenshot_blob_id"] = None
            adjust_references(db, db.execute(
                "SELECT payload_blob_id,screenshot_blob_id FROM save_states WHERE id=?", (row["id"],),
            ).fetchall(), -1)
            columns = list(row)
            db.execute(f"INSERT OR REPLACE INTO save_states({','.join(columns)}) VALUES({','.join('?' for _ in columns)})", list(row.values()))
            adjust_references(db, [(row["payload_blob_id"], row["screenshot_blob_id"])])
        db.execute("UPDATE games SET description=? WHERE id=?", (("公开测试游戏的玩法说明。" * 30) + "\n\n最后一段：完整简介应随页面滚动。", original["game_id"]))
        return original["game_id"]


if __name__ == "__main__":
    print(seed(Path(sys.argv[1])))
