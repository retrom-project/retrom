import contextlib
import importlib.util
import io
import json
import hashlib
import os
import sqlite3
import tempfile
import unittest
import uuid
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / 'check-owned-files.py'
SPEC = importlib.util.spec_from_file_location('owned_files_audit', SCRIPT)
AUDIT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AUDIT)


class OwnedFilesAuditTests(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.TemporaryDirectory()
        self.addCleanup(self.root.cleanup)
        self.path = Path(self.root.name) / 'retrom.db'
        self.ids = [str(uuid.uuid4()), str(uuid.uuid4())]
        with contextlib.closing(sqlite3.connect(self.path)) as db, db:
            db.executescript('''
                CREATE TABLE games(id TEXT PRIMARY KEY,status TEXT);
                CREATE TABLE game_files(game_id TEXT,file_record TEXT);
                CREATE TABLE game_assets(game_id TEXT,file_record TEXT);
                CREATE TABLE game_variants(id TEXT,game_id TEXT);
                CREATE TABLE variant_files(game_variant_id TEXT,file_record TEXT,role TEXT);

            ''')
            self.records = []
            for file_id in self.ids:
                record = json.dumps({"path":f"files/{file_id[-2:]}/{file_id}/content/rom", "size_bytes":4,"sha256":hashlib.sha256(b"same").hexdigest()})
                self.records.append(record)
                db.execute("INSERT INTO games VALUES(?,'PUBLISHED')",(file_id,))
                db.execute('INSERT INTO game_files VALUES(?,?)', (file_id, record))
                path = self.file(file_id)
                path.parent.mkdir(exist_ok=True, parents=True)
                path.write_bytes(b'same')

    def file(self, file_id):
        return self.path.parent / 'files' / file_id[-2:] / file_id / 'content' / 'rom'

    def check(self):
        with contextlib.redirect_stdout(io.StringIO()):
            AUDIT.check(self.path)

    def test_independent_same_bytes_pass(self):
        self.check()

    def test_shared_physical_inode_fails(self):
        self.file(self.ids[1]).unlink()
        os.link(self.file(self.ids[0]), self.file(self.ids[1]))
        with self.assertRaisesRegex(ValueError, 'shares its physical inode'):
            self.check()

    def test_cross_game_ownership_fails(self):
        with contextlib.closing(sqlite3.connect(self.path)) as db, db:
            db.execute("UPDATE game_files SET file_record=? WHERE game_id=?", (self.records[0],self.ids[1]))
        with self.assertRaisesRegex(ValueError, 'crosses game ownership'):
            self.check()

    def test_deleted_game_can_await_directory_removal(self):
        with contextlib.closing(sqlite3.connect(self.path)) as db, db:
            db.execute("UPDATE games SET status='DELETED' WHERE id=?", (self.ids[0],))
        self.file(self.ids[0]).unlink()
        self.check()
