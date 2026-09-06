"""Operator source directory mounting stays separate from private PFB state."""

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from pfb.docker import _server_data_root


class ServerDataDirectoryTests(unittest.TestCase):
    def test_managed_workspace_uses_shared_operator_data(self):
        with tempfile.TemporaryDirectory() as temporary:
            workspace = Path(temporary)
            (workspace / "manifest.yaml").touch()
            baseline = workspace / "project/retrom/.git"
            worktree = workspace / ".worktree/feature/project/retrom"
            with patch("pfb.docker.git_common_dir", return_value=baseline):
                self.assertEqual(_server_data_root(worktree), workspace / ".dev-data")
            self.assertFalse((workspace / ".dev-data").exists())

    def test_standalone_checkout_uses_its_own_operator_data(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "retrom"
            with patch("pfb.docker.git_common_dir", return_value=root / ".git"):
                self.assertEqual(_server_data_root(root), root / ".dev-data")


if __name__ == "__main__":
    unittest.main()
