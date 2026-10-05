"""Temporarily isolate layout-only history inside disposable acceptance data."""
import json
import postgres_fixture as pg
import sys
import uuid
from pathlib import Path
from fixture_files import retire_save


validate_database = pg.validate_database


def clear_history(db, profile):
    db.execute("DELETE FROM profile_game_activity WHERE profile_id=%s", (profile,))
    db.execute("DELETE FROM play_sessions WHERE profile_id=%s", (profile,))
    retire_save(db, "profile_id=%s", (profile,))
    db.execute("DELETE FROM save_states WHERE profile_id=%s", (profile,))


def isolate(path: Path) -> None:
    path = validate_database(path)
    snapshot = path / "ui-layout-state.json"
    if snapshot.exists():
        raise ValueError("UI layout history is already isolated")
    with pg.connect(path) as db:
        db.row_factory = pg.row_factory

        profile = db.execute("SELECT profile_id FROM users WHERE username='test'").fetchone()[0]
        parked = str(uuid.uuid4())
        descriptions = dict(db.execute("SELECT id,description FROM games"))
        # Retain original IDs and foreign references from restored Launches and
        # native-save revisions while keeping them out of the layout user's UI.
        db.execute("INSERT INTO profiles(id,display_name,created_at_ms) VALUES(%s,'UI history holding profile',0)", (parked,))
        for table in ("play_sessions", "profile_game_activity", "save_states"):
            db.execute(f"UPDATE {table} SET profile_id=%s WHERE profile_id=%s", (parked, profile))
        snapshot.write_text(json.dumps({"profile": profile, "parkedProfile": parked, "descriptions": descriptions}))


def restore(path: Path) -> None:
    path = validate_database(path)
    snapshot = path / "ui-layout-state.json"
    if not snapshot.exists():
        return
    saved = json.loads(snapshot.read_text())
    with pg.connect(path) as db:

        if db.execute("SELECT 1 FROM profiles WHERE id=%s", (saved["parkedProfile"],)).fetchone():
            clear_history(db, saved["profile"])
            for table in ("play_sessions", "profile_game_activity", "save_states"):
                db.execute(f"UPDATE {table} SET profile_id=%s WHERE profile_id=%s", (saved["profile"], saved["parkedProfile"]))
            db.execute("DELETE FROM profiles WHERE id=%s", (saved["parkedProfile"],))
            db.cursor().executemany("UPDATE games SET description=%s WHERE id=%s", [(value, key) for key, value in saved["descriptions"].items()])
    snapshot.unlink()


if __name__ == "__main__":
    {"isolate": isolate, "restore": restore}[sys.argv[2]](Path(sys.argv[1]))
