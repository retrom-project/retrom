#!/usr/bin/env python3
"""Seed detail layout states only in the disposable UI acceptance database.

Uses the public screenshot fixture as a layout-only payload; never launch it.
"""
import importlib.util
import sqlite3
import sys
from pathlib import Path
from ui_layout_state import validate_database


def seed(path: Path) -> str:
    validate_database(path)
    spec = importlib.util.spec_from_file_location("ui_home_seed", Path(__file__).with_name("seed-ui-home.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.seed(path, "saved")
    with sqlite3.connect(path) as db:
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA foreign_keys=ON")
        original = dict(db.execute("SELECT * FROM save_states WHERE id=?", (module.SAVE_ID,)).fetchone())
        for index in range(1, 4):
            row = {**original, "id": f"0198ff00-9001-7000-8000-{index:012d}", "name": f"详情布局存档 {index}",
                   "created_at_ms": original["created_at_ms"] - index * 60000}
            if index == 2:
                row["screenshot_blob_id"] = None
            columns = list(row)
            db.execute(f"INSERT OR REPLACE INTO save_states({','.join(columns)}) VALUES({','.join('?' for _ in columns)})", list(row.values()))
        db.execute("UPDATE games SET description=? WHERE id=?", (("公开测试游戏的玩法说明。" * 30) + "\n\n最后一段：完整简介应随页面滚动。", original["game_id"]))
        return original["game_id"]


if __name__ == "__main__":
    print(seed(Path(sys.argv[1])))
