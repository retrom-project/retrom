"""Temporarily isolate layout-only history inside disposable acceptance data."""
import json
import sqlite3
import sys
from pathlib import Path


def validate_database(path: Path) -> Path:
    path = path.resolve()
    if path.name != "retrom.db" or path.parent.name != "data" or not path.parent.parent.name.startswith(
        ("retrom-ui-acceptance.", "retrom-web-e2e.")
    ):
        raise ValueError("UI seed requires the disposable acceptance database")
    return path


def clear_history(db, profile):
    db.execute("DELETE FROM play_session_events WHERE play_session_id IN (SELECT id FROM play_sessions WHERE profile_id=?)", (profile,))
    db.execute("DELETE FROM play_sessions WHERE profile_id=?", (profile,))
    db.execute("DELETE FROM save_states WHERE profile_id=?", (profile,))


def isolate(path: Path) -> None:
    path = validate_database(path)
    snapshot = path.with_name("ui-layout-state.json")
    if snapshot.exists():
        raise ValueError("UI layout history is already isolated")
    with sqlite3.connect(path) as db:
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("BEGIN IMMEDIATE")
        profile = db.execute("SELECT profile_id FROM users WHERE username='test'").fetchone()[0]
        rows = {table: [dict(row) for row in db.execute(f"SELECT * FROM {table} WHERE profile_id=?", (profile,))]
                for table in ("play_sessions", "save_states")}
        rows["play_session_events"] = [dict(row) for row in db.execute(
            "SELECT * FROM play_session_events WHERE play_session_id IN (SELECT id FROM play_sessions WHERE profile_id=?)", (profile,))]
        descriptions = dict(db.execute("SELECT id,description FROM games"))
        snapshot.write_text(json.dumps({"profile": profile, "rows": rows, "descriptions": descriptions}))
        clear_history(db, profile)


def restore(path: Path) -> None:
    path = validate_database(path)
    snapshot = path.with_name("ui-layout-state.json")
    if not snapshot.exists():
        return
    saved = json.loads(snapshot.read_text())
    with sqlite3.connect(path) as db:
        db.execute("PRAGMA foreign_keys=ON")
        db.execute("BEGIN IMMEDIATE")
        clear_history(db, saved["profile"])
        for table in ("play_sessions", "play_session_events", "save_states"):
            for row in saved["rows"][table]:
                columns = ",".join(row)
                placeholders = ",".join("?" for _ in row)
                db.execute(f"INSERT INTO {table}({columns}) VALUES({placeholders})", list(row.values()))
        db.executemany("UPDATE games SET description=? WHERE id=?", [(value, key) for key, value in saved["descriptions"].items()])
    snapshot.unlink()


if __name__ == "__main__":
    {"isolate": isolate, "restore": restore}[sys.argv[2]](Path(sys.argv[1]))
