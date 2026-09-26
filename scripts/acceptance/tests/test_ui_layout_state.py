import importlib.util
import sqlite3
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "ui_layout_state.py"
SPEC = importlib.util.spec_from_file_location("ui_layout_state", SCRIPT)
STATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STATE)


class UILayoutStateTests(unittest.TestCase):
    def test_rejects_persistent_paths_and_symlink_escape(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            for name in ("pfb", "retrom-ui-acceptance", "retrom-web-e2e-backup"):
                with self.assertRaises(ValueError):
                    STATE.validate_database(root / name / "data/retrom.db")
            escaped = root / "retrom-web-e2e.fixture"
            escaped.symlink_to(root)
            with self.assertRaises(ValueError):
                STATE.validate_database(escaped / "data/retrom.db")

    def test_restores_history_events_saves_and_descriptions_without_touching_other_profiles(self):
        with tempfile.TemporaryDirectory(prefix="retrom-web-e2e.") as root:
            path = Path(root) / "data/retrom.db"
            path.parent.mkdir()
            with sqlite3.connect(path) as db:
                db.executescript("""
                    CREATE TABLE users(username TEXT,profile_id TEXT);
                    CREATE TABLE play_sessions(id TEXT PRIMARY KEY,profile_id TEXT);
                    CREATE TABLE play_session_events(play_session_id TEXT REFERENCES play_sessions(id),client_sequence INTEGER);
                    CREATE TABLE save_states(id TEXT PRIMARY KEY,profile_id TEXT);
                    CREATE TABLE games(id TEXT PRIMARY KEY,description TEXT);
                    INSERT INTO users VALUES('test','p');
                    INSERT INTO play_sessions VALUES('play','p'),('other','q');
                    INSERT INTO play_session_events VALUES('play',1),('other',2);
                    INSERT INTO save_states VALUES('save','p'),('other-save','q');
                    INSERT INTO games VALUES('game','original');
                """)
            STATE.isolate(path)
            with self.assertRaisesRegex(ValueError, "already isolated"):
                STATE.isolate(path)
            with sqlite3.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM play_sessions").fetchall(), [("other",)])
                self.assertEqual(db.execute("SELECT id FROM save_states").fetchall(), [("other-save",)])
                db.execute("INSERT INTO play_sessions VALUES('layout','p')")
                db.execute("INSERT INTO save_states VALUES('layout-save','p')")
                db.execute("UPDATE games SET description='layout'")
            STATE.restore(path)
            STATE.restore(path)
            with sqlite3.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM play_sessions ORDER BY id").fetchall(), [("other",), ("play",)])
                self.assertEqual(db.execute("SELECT play_session_id FROM play_session_events ORDER BY play_session_id").fetchall(), [("other",), ("play",)])
                self.assertEqual(db.execute("SELECT id FROM save_states ORDER BY id").fetchall(), [("other-save",), ("save",)])
                self.assertEqual(db.execute("SELECT description FROM games").fetchone(), ("original",))


if __name__ == "__main__":
    unittest.main()
