"""Exercise published review inspection against the authoritative profile DDL."""
import re
import sqlite3
import unittest
from pathlib import Path

from scripts.acceptance.tests.test_rpgmaker_pack_inspect import inspector


class PublishedProfileTests(unittest.TestCase):
    def setUp(self):
        self.db = sqlite3.connect(":memory:")
        self.addCleanup(self.db.close)
        self.db.row_factory = sqlite3.Row
        migration = (Path(__file__).resolve().parents[3] / "migrations/007_library.sql").read_text()
        ddl = re.search(r'CREATE TABLE "rpgmaker_game_profiles" \(.*?\n\);', migration, re.S).group()
        self.db.executescript('''
CREATE TABLE games(id TEXT,status TEXT,content_source_ref_id TEXT);
CREATE TABLE game_variants(game_id TEXT,status TEXT,provider_id TEXT,target_id TEXT);
INSERT INTO games VALUES('game','PUBLISHED','review');
INSERT INTO game_variants VALUES('game','READY','retrom-runtime','rpgmaker-xp');
''' + ddl)
        self.db.execute('''INSERT INTO rpgmaker_game_profiles
(game_id,evidence_family,evidence_generation,evidence_confidence,file_count,total_bytes,
 project_fingerprint,requirements_sha256,analysis_json,created_at_ms,updated_at_ms)
VALUES('game','RGSS','RPGXP','MATCHED',1,10,?,?,'{}',1,1)''', ("a" * 64, "b" * 64))
        self.observed = {"reviews": {"published": [
            {"role": "rpgxpNoRtp", "itemId": "review", "gameId": "game", "generation": "RPGXP"}]}}

    def test_current_profile_evidence_and_selected_target_prove_generation(self):
        result = inspector.published_reviews(self.db, self.observed)
        self.assertEqual(result[0]["generation"], "RPGXP")

    def test_wrong_selected_target_or_changed_source_is_rejected(self):
        for query in ["UPDATE game_variants SET target_id='rpgmaker-vx'",
                      "UPDATE games SET content_source_ref_id='foreign'"]:
            with self.subTest(query=query):
                self.db.execute("SAVEPOINT trial")
                self.db.execute(query)
                with self.assertRaisesRegex(inspector.InspectError, "PUBLISHED_REVIEW_INVALID"):
                    inspector.published_reviews(self.db, self.observed)
                self.db.execute("ROLLBACK TO trial")
