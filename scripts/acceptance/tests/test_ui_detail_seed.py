import importlib.util
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
import postgres_fixture as pg
from fixture_files import file_path, put_owned

SPEC = importlib.util.spec_from_file_location("ui_detail_seed", SCRIPTS / "seed-ui-detail.py")
DETAIL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DETAIL)
LAYOUT_SAVE = "0198ff00-9000-7000-8000-000000000001"
PARKED_SAVE = "0198ff00-9000-7000-8000-000000000009"


class UIDetailSeedTests(unittest.TestCase):
    def test_video_only_preserves_parked_saves_and_their_payloads(self):
        with tempfile.TemporaryDirectory(prefix="retrom-web-e2e.") as root, pg.disposable(Path(root) / "data") as path:
            with pg.connect(path) as db:
                db.execute("""
                    CREATE TABLE games(id TEXT PRIMARY KEY,description TEXT);
                    CREATE TABLE game_assets(game_id TEXT,kind TEXT);
                    CREATE TABLE save_states(id TEXT PRIMARY KEY,game_id TEXT,profile_id TEXT,
                        name TEXT,created_at_ms INTEGER,payload_file_record TEXT,screenshot_file_record TEXT);
                    CREATE TABLE launches(id TEXT PRIMARY KEY,save_state_id TEXT REFERENCES save_states(id));
                    INSERT INTO games VALUES('game','original');
                    INSERT INTO game_assets VALUES('game','VIDEO');
                """)
                for owner, profile in ((LAYOUT_SAVE, "layout"), (PARKED_SAVE, "parked")):
                    record = put_owned(db, b"valid original checkpoint", "SAVE_STATE", owner, "application/octet-stream")
                    db.execute("INSERT INTO save_states VALUES(%s,'game',%s,'save',1000,%s,%s)", (owner, profile, record, record))
                original_path = file_path(db, record)
                db.execute("INSERT INTO launches VALUES('restore',%s)", (PARKED_SAVE,))
            # The home fixture has already prepared the layout save. Exercise
            # the real detail mutation and file ownership against a parked save.
            home = types.SimpleNamespace(SAVE_ID=LAYOUT_SAVE, seed=lambda *_: None)
            spec = types.SimpleNamespace(loader=types.SimpleNamespace(exec_module=lambda _: None))
            with patch.object(DETAIL.importlib.util, "spec_from_file_location", return_value=spec), \
                    patch.object(DETAIL.importlib.util, "module_from_spec", return_value=home):
                self.assertEqual(DETAIL.seed(path, "video"), "game")
            with pg.connect(path) as db:
                self.assertEqual(db.execute("SELECT id FROM save_states").fetchall(), [(PARKED_SAVE,)])
                self.assertEqual(db.execute("SELECT save_state_id FROM launches").fetchone(), (PARKED_SAVE,))
                self.assertEqual(db.execute("SELECT conname FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND NOT convalidated").fetchall(), [])
                self.assertEqual(db.execute("SELECT kind FROM game_assets").fetchone(), ("VIDEO",))
            self.assertEqual(original_path.read_bytes(), b"valid original checkpoint")


if __name__ == "__main__":
    unittest.main()
