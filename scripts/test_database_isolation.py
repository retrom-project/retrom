"""Regression coverage for database isolation source and configuration checks."""

from pathlib import Path
import subprocess
import tempfile
import unittest

from database_isolation import check, violations


class DatabaseIsolationTest(unittest.TestCase):
    def test_driver_sql_and_connection_settings(self):
        for source in (
            "sql.LevelSerializable", "driver.LevelLinearizable",
            "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE",
            "set session characteristics as transaction isolation level serializable",
            "SELECT set_config('transaction_isolation', 'serializable', true)",
            "postgres://local/db?default_transaction_isolation=serializable",
        ):
            with self.subTest(source=source):
                self.assertEqual(violations(source), [1])

    def test_supported_isolation(self):
        for source in ("sql.LevelRepeatableRead", "sql.LevelReadCommitted",
                       "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"):
            self.assertEqual(violations(source), [])

    def test_gate_covers_tracked_and_new_application_inputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            (root / "adapter.go").write_text("package db\nvar level = sql.LevelSerializable\n")
            subprocess.run(["git", "-C", str(root), "add", "adapter.go"], check=True)
            (root / "runtime.env").write_text("PGOPTIONS=--default-transaction-isolation=serializable\n")
            failures = check(root)
            self.assertEqual(len(failures), 2)
            self.assertTrue(any("adapter.go:2:" in value for value in failures))
            self.assertTrue(any("runtime.env:1:" in value for value in failures))


if __name__ == "__main__":
    unittest.main()
