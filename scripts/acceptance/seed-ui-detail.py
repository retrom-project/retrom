#!/usr/bin/env python3
"""Seed detail layout states only in the disposable UI acceptance database.

Uses the public screenshot fixture as a layout-only payload; never launch it.
"""
import importlib.util
import sqlite3
import sys
from pathlib import Path
from fixture_files import own_rows, put_owned, retire_save
from ui_layout_state import validate_database


def ensure_video(db, database_path, game_id, timestamp):
    if db.execute("SELECT 1 FROM game_assets WHERE game_id=? AND kind='VIDEO'", (game_id,)).fetchone():
        return
    root = Path(__file__).resolve().parents[2]
    contents = (root / "testdata/public-roms/gba-smoke/emulationstation-smoke-video.webm").read_bytes()
    file_record = put_owned(db, contents, "GAME", game_id, "video/webm")
    db.execute(
        "INSERT INTO game_assets(id,game_id,file_record,kind,ordinal,media_type,created_at_ms) "
        "VALUES('0198ff00-9002-7000-8000-000000000002',?,?,'VIDEO',0,'video/webm',?)",
        (game_id, file_record, timestamp),
    )


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
                row["screenshot_file_record"] = None
            retire_save(db, "id=?", (row["id"],))
            columns = list(row)
            db.execute(f"INSERT OR REPLACE INTO save_states({','.join(columns)}) VALUES({','.join('?' for _ in columns)})", list(row.values()))
            own_rows(db, "save_states", "id", row["id"], "SAVE_STATE", row["id"], ("payload_file_record", "screenshot_file_record"))
        db.execute("UPDATE games SET description=? WHERE id=?", (("公开测试游戏的玩法说明。" * 30) + "\n\n最后一段：完整简介应随页面滚动。", original["game_id"]))
        return original["game_id"]


if __name__ == "__main__":
    print(seed(Path(sys.argv[1])))
