import importlib.util
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "ui_layout_state.py"
sys.path.insert(0, str(SCRIPT.parent))
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
                    CREATE TABLE profiles(id TEXT PRIMARY KEY,display_name TEXT,created_at_ms INTEGER);
                    CREATE TABLE users(username TEXT,profile_id TEXT);
                    CREATE TABLE play_sessions(id TEXT PRIMARY KEY,profile_id TEXT REFERENCES profiles(id));
                    CREATE TABLE play_session_events(play_session_id TEXT REFERENCES play_sessions(id),client_sequence INTEGER);
                    CREATE TABLE stored_files(id TEXT PRIMARY KEY,owner_kind TEXT,owner_id TEXT,retired_at_ms INTEGER);
                    CREATE TABLE file_deletions(blob_id TEXT PRIMARY KEY REFERENCES stored_files(id));
                    CREATE TABLE save_states(id TEXT PRIMARY KEY,profile_id TEXT REFERENCES profiles(id),
                        payload_blob_id TEXT REFERENCES stored_files(id),screenshot_blob_id TEXT REFERENCES stored_files(id));
                    CREATE TABLE launches(id TEXT PRIMARY KEY,save_state_id TEXT REFERENCES save_states(id));
                    CREATE TABLE games(id TEXT PRIMARY KEY,description TEXT);
                    INSERT INTO profiles VALUES('p','Test',0),('q','Other',0);
                    INSERT INTO users VALUES('test','p');
                    INSERT INTO play_sessions VALUES('play','p'),('other','q');
                    INSERT INTO play_session_events VALUES('play',1),('other',2);
                    INSERT INTO stored_files VALUES('blob','SAVE_STATE','save',NULL),('other-blob','SAVE_STATE','other-save',NULL),('layout-blob','SAVE_STATE','layout-save',NULL);
                    INSERT INTO save_states VALUES('save','p','blob','blob'),('other-save','q','other-blob','other-blob');
                    INSERT INTO launches VALUES('restored','save');
                    INSERT INTO games VALUES('game','original');
                """)
            STATE.isolate(path)
            with self.assertRaisesRegex(ValueError, "already isolated"):
                STATE.isolate(path)
            with sqlite3.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM play_sessions WHERE profile_id='p'").fetchall(), [])
                self.assertEqual(db.execute("SELECT id FROM save_states WHERE profile_id='p'").fetchall(), [])
                self.assertEqual(db.execute("SELECT id FROM save_states WHERE profile_id='q'").fetchall(), [("other-save",)])
                db.execute("INSERT INTO play_sessions VALUES('layout','p')")
                db.execute("INSERT INTO save_states VALUES('layout-save','p','layout-blob','layout-blob')")
                db.execute("UPDATE games SET description='layout'")
            STATE.restore(path)
            STATE.restore(path)
            with sqlite3.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM play_sessions ORDER BY id").fetchall(), [("other",), ("play",)])
                self.assertEqual(db.execute("SELECT play_session_id FROM play_session_events ORDER BY play_session_id").fetchall(), [("other",), ("play",)])
                self.assertEqual(db.execute("SELECT id FROM save_states ORDER BY id").fetchall(), [("other-save",), ("save",)])
                self.assertEqual(db.execute("SELECT description FROM games").fetchone(), ("original",))
                self.assertEqual(db.execute("SELECT save_state_id FROM launches").fetchone(), ("save",))
                self.assertEqual(db.execute("PRAGMA foreign_key_check").fetchall(), [])
                self.assertEqual(db.execute("SELECT id FROM stored_files WHERE retired_at_ms IS NULL ORDER BY id").fetchall(), [("blob",), ("other-blob",)])
                self.assertIsNotNone(db.execute("SELECT retired_at_ms FROM stored_files WHERE id='layout-blob'").fetchone()[0])


if __name__ == "__main__":
    unittest.main()
