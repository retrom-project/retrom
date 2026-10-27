import contextlib
from copy import deepcopy
import io
from pathlib import Path
import subprocess
import tempfile
import unittest

import migrations


class PublishedMigrationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git("init", "-q")
        self.git("config", "user.name", "Migration Test")
        self.git("config", "user.email", "test@example.invalid")
        self.write("001_schema.sql", "CREATE TABLE example_tab(id TEXT);")
        self.git("add", ".")
        self.git("commit", "-qm", "published schema")
        self.manifest = migrations.snapshot(self.root, "HEAD", "v1.0.0")

    def git(self, *arguments):
        return subprocess.check_output(["git", "-C", str(self.root), *arguments], stderr=subprocess.PIPE)

    def write(self, name, text):
        path = self.root / "migrations" / name
        path.parent.mkdir(exist_ok=True)
        path.write_text(text)

    def check(self, manifest=None):
        with contextlib.redirect_stdout(io.StringIO()):
            migrations.check(self.root, manifest or self.manifest)

    def test_published_bytes_cannot_be_replaced_or_deleted(self):
        for content in ("SELECT 1;", None):
            with self.subTest(content=content):
                if content is None:
                    (self.root / "migrations/001_schema.sql").unlink()
                else:
                    self.write("001_schema.sql", content)
                with self.assertRaisesRegex(ValueError, "published migration|migration list is empty"):
                    self.check()

    def test_unpublished_migrations_remain_editable(self):
        self.write("002_next.sql", "SELECT 1;")
        self.check()
        self.write("002_next.sql", "SELECT 2;")
        self.check()
        self.git("add", ".")
        self.git("commit", "-qm", "published addition")
        released = migrations.snapshot(self.root, "HEAD", "v1.1.0")
        self.write("002_next.sql", "SELECT 3;")
        with self.assertRaisesRegex(ValueError, "002_next.sql"):
            self.check(released)

    def test_duplicate_gap_and_unsafe_paths_are_rejected(self):
        for name in ("001_duplicate.sql", "003_gap.sql", "unregistered.sql"):
            with self.subTest(name=name):
                self.write(name, "SELECT 1;")
                with self.assertRaisesRegex(ValueError, "consecutive number"):
                    self.check()
                (self.root / "migrations" / name).unlink()
        altered = deepcopy(self.manifest)
        altered["migrations"][0]["path"] = "../001_schema.sql"
        with self.assertRaisesRegex(ValueError, "invalid published migration entry"):
            self.check(altered)


if __name__ == "__main__":
    unittest.main()
