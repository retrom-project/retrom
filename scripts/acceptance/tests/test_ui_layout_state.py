import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "ui_layout_state.py"
sys.path.insert(0, str(SCRIPT.parent))
import postgres_fixture as pg
SPEC = importlib.util.spec_from_file_location("ui_layout_state", SCRIPT)
STATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STATE)


class UILayoutStateTests(unittest.TestCase):
    def test_rejects_persistent_paths_and_symlink_escape(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            for name in ("pfb", "retrom-ui-acceptance", "retrom-web-e2e-backup"):
                with self.assertRaises(ValueError):
                    STATE.validate_database(root / name / "data")
            escaped = root / "retrom-web-e2e.fixture"
            escaped.symlink_to(root)
            with self.assertRaises(ValueError):
                STATE.validate_database(escaped / "data")

    def test_restores_history_saves_and_descriptions_without_touching_other_profiles(self):
        with tempfile.TemporaryDirectory(prefix="retrom-web-e2e.") as root, pg.disposable(Path(root) / "data") as path:
            with pg.connect(path) as db:
                db.execute("""
                    CREATE TABLE profiles(id TEXT PRIMARY KEY,display_name TEXT,created_at_ms INTEGER);
                    CREATE TABLE users(username TEXT,profile_id TEXT);
                    CREATE TABLE play_sessions(id TEXT PRIMARY KEY,profile_id TEXT REFERENCES profiles(id));
                    CREATE TABLE profile_game_activity(profile_id TEXT REFERENCES profiles(id),game_id TEXT,
                        active_duration_ms INTEGER,session_count INTEGER,PRIMARY KEY(profile_id,game_id));
                    CREATE TABLE save_states(id TEXT PRIMARY KEY,profile_id TEXT REFERENCES profiles(id),payload_file_record TEXT,screenshot_file_record TEXT);
                    CREATE TABLE launches(id TEXT PRIMARY KEY,save_state_id TEXT REFERENCES save_states(id));
                    CREATE TABLE games(id TEXT PRIMARY KEY,description TEXT);
                    INSERT INTO profiles VALUES('p','Test',0),('q','Other',0);
                    INSERT INTO users VALUES('test','p');
                    INSERT INTO play_sessions VALUES('play','p'),('other','q');
                    INSERT INTO profile_game_activity VALUES('p','game',1000,1),('q','game',2000,2);
                    INSERT INTO save_states VALUES('save','p','blob','blob'),('other-save','q','other-blob','other-blob');
                    INSERT INTO launches VALUES('restored','save');
                    INSERT INTO games VALUES('game','original');
                """)
            STATE.isolate(path)
            with self.assertRaisesRegex(ValueError, "already isolated"):
                STATE.isolate(path)
            with pg.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM play_sessions WHERE profile_id='p'").fetchall(), [])
                self.assertEqual(db.execute("SELECT * FROM profile_game_activity WHERE profile_id='p'").fetchall(), [])
                self.assertEqual(db.execute("SELECT active_duration_ms FROM profile_game_activity WHERE profile_id='q'").fetchone(), (2000,))
                self.assertEqual(db.execute("SELECT id FROM save_states WHERE profile_id='p'").fetchall(), [])
                self.assertEqual(db.execute("SELECT id FROM save_states WHERE profile_id='q'").fetchall(), [("other-save",)])
                db.execute("INSERT INTO play_sessions VALUES('layout','p')")
                db.execute("INSERT INTO profile_game_activity VALUES('p','layout',3000,3)")
                db.execute("INSERT INTO save_states VALUES('018fbe68-0000-7000-8000-000000000001','p','layout-blob','layout-blob')")
                db.execute("UPDATE games SET description='layout'")
            STATE.restore(path)
            STATE.restore(path)
            with pg.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM play_sessions ORDER BY id").fetchall(), [("other",), ("play",)])
                self.assertEqual(db.execute("SELECT * FROM profile_game_activity ORDER BY profile_id").fetchall(),
                                 [('p', 'game', 1000, 1), ('q', 'game', 2000, 2)])
                self.assertEqual(db.execute("SELECT id FROM save_states ORDER BY id").fetchall(), [("other-save",), ("save",)])
                self.assertEqual(db.execute("SELECT description FROM games").fetchone(), ("original",))
                self.assertEqual(db.execute("SELECT save_state_id FROM launches").fetchone(), ("save",))
                self.assertEqual(db.execute("SELECT conname FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND NOT convalidated").fetchall(), [])


if __name__ == "__main__":
    unittest.main()
